package provider

import (
	"context"
	"errors"
	"net/http/httptrace"
	"sync/atomic"
	"time"

	"websearch/pkg/admission"
	"websearch/pkg/quota"
	"websearch/pkg/telemetry"
)

type searchQuota struct {
	provider string
	service  *quota.Service
}

// SetSearchQuota attaches the runtime budget to a pool before sharing the pool.
// Fetch adapters keep using NextUsable without spending search allowance.
func (p *KeyPool) SetSearchQuota(provider string, service *quota.Service) {
	if service.SearchEnforced(provider) {
		p.searchQuota = &searchQuota{provider: provider, service: service}
	}
}

func (p *KeyPool) searchKey(ctx context.Context, name, query string) (context.Context, string, func(bool), error) {
	if p.searchQuota == nil {
		if key, ok := p.NextUsable(); ok {
			return ctx, key, func(bool) {}, nil
		}
		return ctx, "", nil, NoKeyError(name)
	}
	var skip *quota.SkipError
	guard := p.searchQuota
	for _, key := range p.searchCandidates() {
		finish, err := guard.service.AdmitSearch(ctx, guard.provider, key)
		if err == nil {
			var wroteHeaders atomic.Bool
			traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteHeaders: func() { wroteHeaders.Store(true) }})
			return traced, key, func(responseReceived bool) { finish(responseReceived || wroteHeaders.Load()) }, nil
		}
		if !errors.As(err, &skip) {
			return ctx, "", nil, err
		}
		if skip.Reason != quota.SkipOfficial {
			break
		}
	}
	if skip == nil {
		return ctx, "", nil, NoKeyError(name)
	}
	p.recordQuotaSkip(ctx, query, skip.Reason)
	return ctx, "", nil, skip
}

// searchCandidates rotates the starting key once per call, then tries each
// currently usable key at most once when the official allowance blocks a key.
func (p *KeyPool) searchCandidates() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	n := uint64(len(p.keys))
	start := p.idx.Add(1) - 1
	candidates := make([]string, 0, n)
	for i := uint64(0); i < n; i++ {
		index := (start + i) % n
		if !now.Before(p.states[index].invalidUntil) {
			candidates = append(candidates, p.keys[index])
		}
	}
	return candidates
}

func (p *KeyPool) recordQuotaSkip(ctx context.Context, query, reason string) {
	if admission.IsDiagnostic(ctx) {
		return
	}
	telemetry.RecordEventContext(ctx, telemetry.Event{
		Kind: telemetry.QuotaSkipEvent, Tool: "smartsearch", Provider: p.searchQuota.provider, Query: query, Detail: reason,
	})
}
