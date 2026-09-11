// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/rs/zerolog/log"
)

// PurgeCounts is how many rows a purge deleted from each history table.
type PurgeCounts struct {
	SpeedTests int64 `json:"speedTests"`
	PacketLoss int64 `json:"packetLoss"`
	DNS        int64 `json:"dns"`
	Uptime     int64 `json:"uptime"`
}

// purgeTables lists each history table with the field of PurgeCounts it fills.
var purgeTables = []struct {
	table string
	count func(*PurgeCounts) *int64
}{
	{"speed_tests", func(c *PurgeCounts) *int64 { return &c.SpeedTests }},
	{"packet_loss_results", func(c *PurgeCounts) *int64 { return &c.PacketLoss }},
	{"dns_results", func(c *PurgeCounts) *int64 { return &c.DNS }},
	{"uptime_results", func(c *PurgeCounts) *int64 { return &c.Uptime }},
}

// PurgeHistoricalData deletes speed test, packet loss, DNS, and uptime results
// older than the given cutoff. On SQLite it runs VACUUM afterwards so the
// on-disk file actually shrinks (VACUUM cannot run inside a transaction). A
// failed VACUUM is logged but does not fail the call since the rows are already
// gone.
func (s *service) PurgeHistoricalData(ctx context.Context, before time.Time) (PurgeCounts, error) {
	log.Info().Time("before", before).Msg("Purging historical data")

	var counts PurgeCounts

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return counts, err
	}
	defer tx.Rollback()

	for _, t := range purgeTables {
		res, err := s.sqlBuilder.Delete(t.table).Where(sq.Lt{"created_at": before}).RunWith(tx).ExecContext(ctx)
		if err != nil {
			log.Error().Err(err).Str("table", t.table).Msg("Failed to purge historical rows")
			return PurgeCounts{}, err
		}
		*t.count(&counts), _ = res.RowsAffected()
	}

	if err := tx.Commit(); err != nil {
		log.Error().Err(err).Msg("Failed to commit purge transaction")
		return PurgeCounts{}, err
	}

	log.Info().
		Int64("speed_tests", counts.SpeedTests).
		Int64("packet_loss", counts.PacketLoss).
		Int64("dns_results", counts.DNS).
		Int64("uptime_results", counts.Uptime).
		Msg("Purged historical data")

	if s.config.Type == "sqlite" {
		if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
			log.Warn().Err(err).Msg("Failed to VACUUM after purge; database file will not shrink")
		} else if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			// in WAL mode the file only shrinks once the WAL is checkpointed
			log.Warn().Err(err).Msg("Failed to checkpoint WAL after purge")
		}
	}

	return counts, nil
}
