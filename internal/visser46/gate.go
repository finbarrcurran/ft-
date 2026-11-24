// Package visser46 is Jordi-46 Part B: CoinGecko price data for the 46-name Visser-Labs index (crypto_index_members,
// index_key visser46). It backfills a year of daily closes, refreshes one /coins/markets snapshot every 30
// minutes, appends a daily close from that snapshot, and computes breadth from the stored closes.
//
// Scope fence (Fin and ai, 2026-11-15): nothing here reads or writes the watchlist, stock_holdings,
// crypto_holdings, theses, alerts or entry-zone logic, and Part B raises no price alert. The only writes are
// price_history rows (kind crypto / stock) and the crypto_index_snapshot table.
//
// Every CoinGecko call goes through Client, and Client asks Gate before EACH call (each token of the backfill,
// each retry, each refresh): the kill switch preference coingecko_partb_enabled (default off) and the monthly
// soft stop on keyed calls.
package visser46

import (
	"context"
	"errors"
	"fmt"
	"ft/internal/market"
	"time"
)

// Why a call is refused.
var (
	ErrKillSwitchOff = errors.New("visser46: kill switch is off (coingecko_partb_enabled != on)")
	ErrSoftStop      = errors.New("visser46: monthly keyed soft stop reached")
	ErrNoKey         = errors.New("visser46: no CoinGecko Demo key configured (Part B calls are keyed only)")
	ErrExemptUsed    = errors.New("visser46: today's one exempt daily-close call was already made")
)

// Settings is what Gate reads; *store.Store satisfies it.
type Settings interface {
	PartBEnabled(ctx context.Context) bool
	PartBSoftStop(ctx context.Context) int
	CoinGeckoMonthKeyed(ctx context.Context, at time.Time) (int, error)
}

// Gate decides whether Part B may make a CoinGecko call right now.
type Gate struct {
	S      Settings
	HasKey func() bool // default market.CoinGeckoKeyed
	Now    func() time.Time
}

// Allow returns nil when a call may be made: the switch is on, a key is configured, and the keyed calls so
// far this UTC month are below the soft stop. It fails closed: a counter that cannot be read refuses.
func (g *Gate) Allow(ctx context.Context) error {
	if !g.S.PartBEnabled(ctx) {
		return ErrKillSwitchOff
	}
	has := g.HasKey
	if has == nil {
		has = market.CoinGeckoKeyed
	}
	if !has() {
		return ErrNoKey
	}
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	n, err := g.S.CoinGeckoMonthKeyed(ctx, now().UTC())
	if err != nil {
		return fmt.Errorf("visser46: cannot read the monthly keyed counter (refusing): %w", err)
	}
	if limit := g.S.PartBSoftStop(ctx); n >= limit {
		return fmt.Errorf("%w (%d of %d)", ErrSoftStop, n, limit)
	}
	return nil
}

// AllowExempt is Allow without the soft stop: the kill switch and the key still apply. Used only by the daily
// close's single exempt call.
func (g *Gate) AllowExempt(ctx context.Context) error {
	if err := g.KillSwitchOnly(ctx); err != nil {
		return err
	}
	has := g.HasKey
	if has == nil {
		has = market.CoinGeckoKeyed
	}
	if !has() {
		return ErrNoKey
	}
	return nil
}

// KillSwitchOnly reports whether the switch itself is on (used by the Yahoo stock step, which is not a
// CoinGecko call and so does not count against the keyed budget).
func (g *Gate) KillSwitchOnly(ctx context.Context) error {
	if !g.S.PartBEnabled(ctx) {
		return ErrKillSwitchOff
	}
	return nil
}
