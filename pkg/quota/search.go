package quota

import (
	"context"
	"sync"
	"time"

	"websearch/pkg/errcode"
	"websearch/pkg/i18n"
)

const (
	// A single provider's official snapshot is cached independently of Dashboard.
	searchQuotaTTL     = 90 * time.Second
	searchUsageTimeout = 2 * time.Second

	SkipLocalLimit   = "local_limit"
	SkipOfficial     = "official_limit"
	SkipUnavailable  = "usage_unavailable"
	SkipYuanbaoLimit = "yuanbao_monthly_limit"
)

// SkipError means the upstream was deliberately never contacted for search.
type SkipError struct{ Reason string }

func (e *SkipError) Error() string           { return SearchSkipMessage(i18n.Default(), e.Reason) }
func (e *SkipError) ErrorCode() errcode.Code { return errcode.QuotaSkip }

func SearchSkipMessage(loc *i18n.Localizer, reason string) string {
	switch reason {
	case SkipLocalLimit:
		return loc.T("dashboard.quota.search_local_limit")
	case SkipOfficial:
		return loc.T("dashboard.quota.search_official_limit")
	case SkipUnavailable:
		return loc.T("dashboard.quota.search_usage_unavailable")
	case SkipYuanbaoLimit:
		return loc.T("dashboard.quota.search_yuanbao_limit")
	default:
		return ""
	}
}

type searchGuard struct {
	mu          sync.Mutex
	initialized bool
	poll        chan struct{}
	snapshots   map[string]searchSnapshot
	completed   uint64
	pending     uint64
	yuanbaoTill time.Time
}

type searchSnapshot struct {
	item      Item
	checkedAt time.Time
	completed uint64
	window    int64
}

func (s *Service) SearchEnforced(provider string) bool {
	return s != nil && s.routes[provider] != nil
}

func (s *Service) searchWindow(now time.Time) int64 {
	start := s.conf.Dashboard.Quotas.PeriodStart(now)
	if start.IsZero() {
		return 0
	}
	return start.UnixNano()
}

func (s *Service) localSearchLimit(provider string) int64 {
	return int64(s.conf.Dashboard.Quotas.GetLimit(provider)) * int64(max(1, len(usableProviderKeys(s.conf, provider))))
}

// AdmitSearch reserves one outbound search in the durable local period and in
// the latest official snapshot, when available for the selected credential.
// Calling finish(false) releases the reservation for a request that never sent.
func (s *Service) AdmitSearch(ctx context.Context, provider, key string) (finish func(bool), err error) {
	route := s.routes[provider]
	if route == nil {
		return func(bool) {}, nil
	}
	if s.store == nil {
		return nil, &SkipError{Reason: SkipUnavailable}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Avoid a quota API call if the configured hard limit already disallows this search.
	if reason := s.localBlockReason(provider); reason != "" {
		return nil, &SkipError{Reason: reason}
	}
	if s.officialSearchUsageAvailable(provider) {
		if err := s.refreshSearchSnapshot(ctx, route, provider, key); err != nil {
			return nil, err
		}
	}

	route.mu.Lock()
	defer route.mu.Unlock()
	now := s.clock()
	snapshot := route.snapshots[key]
	if s.officialLimitReached(provider, route, snapshot, now) {
		return nil, &SkipError{Reason: SkipOfficial}
	}
	revision, ok, err := s.store.ReserveSearchQuota(ctx, provider, s.searchWindow(now), s.localSearchLimit(provider))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &SkipError{Reason: SkipUnavailable}
	}
	if !ok {
		return nil, &SkipError{Reason: SkipLocalLimit}
	}
	route.pending++
	var once sync.Once
	return func(sent bool) {
		once.Do(func() {
			route.mu.Lock()
			defer route.mu.Unlock()
			route.pending--
			if sent {
				route.completed++
			} else {
				_ = s.store.ReleaseSearchQuota(provider, revision)
			}
		})
	}, nil
}

func (s *Service) localBlockReason(provider string) string {
	used, err := s.searchUsed(provider, s.clock())
	if err != nil {
		return SkipUnavailable
	}
	if used >= s.localSearchLimit(provider) {
		return SkipLocalLimit
	}
	return ""
}

func (s *Service) searchUsed(provider string, now time.Time) (int64, error) {
	if s.store == nil {
		return 0, &SkipError{Reason: SkipUnavailable}
	}
	route := s.routes[provider]
	route.mu.Lock()
	defer route.mu.Unlock()
	if !route.initialized {
		if err := s.seedLegacySearchQuota(provider, now); err != nil {
			return 0, err
		}
		route.initialized = true
	}
	return s.store.SearchQuotaUsage(provider, s.searchWindow(now))
}

func (s *Service) seedLegacySearchQuota(provider string, now time.Time) error {
	if present, err := s.store.HasSearchQuota(provider); present || err != nil {
		return err
	}
	window := s.searchWindow(now)
	periodStart, adjust, err := s.store.QuotaState(provider)
	if err != nil {
		return err
	}
	if window != 0 && periodStart < time.Unix(0, window).Unix() {
		periodStart, adjust = time.Unix(0, window).Unix(), 0
	}
	names := []string{provider}
	switch provider {
	case "baidu":
		names = append(names, "baidu_api", "baidu_ai")
	case providerTavily:
		names = append(names, "tavily_api")
	case providerTinyfish:
		names = append(names, "tinyfish_api")
	case "keenable":
		names = append(names, "keenable_api")
	}
	// Older telemetry did not separate hosted/anonymous routes from a same-name
	// direct provider. Preserve the observed baseline conservatively once.
	used := adjust
	if periodStart > 0 || window != 0 {
		for _, name := range names {
			count, err := s.store.CountProviderCalls(name, periodStart)
			if err != nil {
				return err
			}
			used += count
		}
	}
	if periodStart > 0 {
		window = time.Unix(periodStart, 0).UnixNano()
	}
	return s.store.InitializeSearchQuota(provider, window, max(0, used))
}

func (s *Service) officialSearchUsageAvailable(provider string) bool {
	// TinyFish exposes a usage count, not an official daily search cap.
	// Match the configured accounting window and skip cross-key inference.
	return provider == providerTavily ||
		(provider == providerTinyfish && len(usableProviderKeys(s.conf, provider)) == 1)
}

func (s *Service) refreshSearchSnapshot(ctx context.Context, route *searchGuard, provider, key string) error {
	for {
		var window int64
		if provider == providerTinyfish {
			var err error
			window, err = s.store.SearchQuotaPeriodStart(provider, s.searchWindow(s.clock()))
			if err != nil || window == 0 {
				return nil
			}
		}
		route.mu.Lock()
		snap := route.snapshots[key]
		if snap.window == window && snap.current(s.clock()) {
			route.mu.Unlock()
			return nil
		}
		if pending := route.poll; pending != nil {
			route.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		pending := make(chan struct{})
		route.poll = pending
		completed := route.completed
		route.mu.Unlock()
		var item Item
		pollCtx, cancel := context.WithTimeout(ctx, searchUsageTimeout)
		if provider == providerTavily {
			item = s.tavilyForKey(pollCtx, key)
		} else {
			now := s.clock()
			item = s.tinyfishSearchItemAt(pollCtx, key, time.Unix(0, window).UTC())
			limit := float64(s.localSearchLimit(provider))
			item.Limit = &limit
			item.ResetAt = ""
			if reset := s.conf.Dashboard.Quotas.NextReset(now); !reset.IsZero() {
				item.ResetAt = reset.Format(time.RFC3339)
			}
		}
		cancel()
		route.mu.Lock()
		if ctx.Err() == nil {
			route.snapshots[key] = searchSnapshot{item: item, checkedAt: s.clock(), completed: completed, window: window}
		}
		close(pending)
		route.poll = nil
		route.mu.Unlock()
		return ctx.Err()
	}
}

func (snap searchSnapshot) current(now time.Time) bool {
	if snap.checkedAt.IsZero() || now.Sub(snap.checkedAt) >= searchQuotaTTL {
		return false
	}
	reset, err := time.Parse(time.RFC3339, snap.item.ResetAt)
	return err != nil || now.Before(reset)
}

func (s *Service) officialLimitReached(provider string, route *searchGuard, snap searchSnapshot, now time.Time) bool {
	if provider == providerTinyfish {
		window, err := s.store.SearchQuotaPeriodStart(provider, s.searchWindow(now))
		if err != nil || window == 0 || snap.window != window {
			return false
		}
	}
	item := snap.item
	if !snap.current(now) || item.Status != "available" || item.Used == nil || item.Limit == nil || *item.Limit <= 0 {
		return false
	}
	// The official endpoint does not expose a shared-account ID. Subtract this
	// process's reservations conservatively without summing different keys' balances.
	reserved := float64(route.completed-snap.completed+route.pending) + 1
	if *item.Used+reserved > *item.Limit {
		return true
	}
	if provider == providerTavily {
		keyUsed, _ := item.Details["key_usage"].(float64)
		keyLimit, _ := item.Details["key_limit"].(float64)
		return keyLimit > 0 && keyUsed+reserved > keyLimit
	}
	return false
}

// SearchBlockReason reports current route evidence for the source table without
// performing any upstream query. Health and failure counts are independent.
func (s *Service) SearchBlockReason(provider string) string {
	if !s.SearchEnforced(provider) || !providerKeyConfigured(s.conf, provider) {
		return ""
	}
	if reason := s.localBlockReason(provider); reason != "" {
		return reason
	}
	route := s.routes[provider]
	route.mu.Lock()
	defer route.mu.Unlock()
	now := s.clock()
	switch provider {
	case "yuanbao":
		if now.Before(route.yuanbaoTill) {
			return SkipYuanbaoLimit
		}
	case providerTavily, providerTinyfish:
		if !s.officialSearchUsageAvailable(provider) {
			return ""
		}
		for _, key := range usableProviderKeys(s.conf, provider) {
			if !s.officialLimitReached(provider, route, route.snapshots[key], now) {
				return ""
			}
		}
		return SkipOfficial
	}
	return ""
}

// NoteYuanbaoLimit reports the independently enforced Yuanbao budget without
// overriding it when the Dashboard counter is reset.
func (s *Service) NoteYuanbaoLimit() {
	if !s.SearchEnforced("yuanbao") {
		return
	}
	now := s.clock()
	route := s.routes["yuanbao"]
	route.mu.Lock()
	route.yuanbaoTill = time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
	route.mu.Unlock()
}
