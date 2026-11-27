package telemetry

import (
	"context"
	"database/sql"
	"errors"
)

// InitializeSearchQuota copies a legacy observation once. An existing reservation
// or administrative update always takes priority over legacy evidence.
func (s *Store) InitializeSearchQuota(provider string, periodStart, used int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO search_quota(provider,period_start,used,revision) VALUES(?,?,?,1)`, provider, periodStart, used)
	return err
}

func (s *Store) HasSearchQuota(provider string) (bool, error) {
	var present bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM search_quota WHERE provider=?)`, provider).Scan(&present)
	return present, err
}

// SearchQuotaUsage reads the separate count for admitted keyed search traffic.
// The count survives detail retention and is scoped to one configured window.
func (s *Store) SearchQuotaUsage(provider string, periodStart int64) (int64, error) {
	var used int64
	err := s.db.QueryRow(`SELECT used FROM search_quota WHERE provider=? AND period_start>=?`, provider, periodStart).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return used, err
}

// SearchQuotaPeriodStart returns the active natural or manually reset window.
func (s *Store) SearchQuotaPeriodStart(provider string, periodStart int64) (int64, error) {
	var stored int64
	err := s.db.QueryRow(`SELECT period_start FROM search_quota WHERE provider=?`, provider).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return periodStart, nil
	}
	return max(periodStart, stored), err
}

// ReserveSearchQuota atomically spends one call within the provider's budget.
// A changing revision makes rollback safe across manual and automatic resets.
func (s *Store) ReserveSearchQuota(ctx context.Context, provider string, periodStart, limit int64) (revision int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `INSERT INTO search_quota(provider,period_start,used,revision) VALUES(?,?,1,1)
		ON CONFLICT(provider) DO UPDATE SET
		period_start=max(search_quota.period_start,excluded.period_start),
		used=CASE WHEN search_quota.period_start<excluded.period_start THEN 1 ELSE search_quota.used+1 END,
		revision=search_quota.revision+(search_quota.period_start<excluded.period_start)
		WHERE search_quota.period_start<excluded.period_start OR search_quota.used<?
		RETURNING revision`, provider, periodStart, limit).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return revision, err == nil, err
}

// ReleaseSearchQuota returns a reservation that never went to the upstream.
func (s *Store) ReleaseSearchQuota(provider string, revision int64) error {
	_, err := s.db.Exec(`UPDATE search_quota SET used=max(used-1,0) WHERE provider=? AND revision=?`, provider, revision)
	return err
}

// SetSearchQuotaUsed sets the locally observed count at an administrative boundary.
func (s *Store) SetSearchQuotaUsed(provider string, periodStart, used int64) error {
	_, err := s.db.Exec(`INSERT INTO search_quota(provider,period_start,used,revision) VALUES(?,?,?,1)
		ON CONFLICT(provider) DO UPDATE SET
		period_start=excluded.period_start,used=excluded.used,revision=search_quota.revision+1`, provider, periodStart, used)
	return err
}
