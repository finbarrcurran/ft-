package server

import (
	"context"
	"strings"
	"testing"
)

// FT-144: the drawdown basis text says how many days still use today's cash balance, and none after the backfill.
func TestDrawdownBasisText(t *testing.T) {
	if got := drawdownBasis(0, 173); got != "incl. cash (recorded for every day)" {
		t.Fatalf("no gaps: %q", got)
	}
	if got := drawdownBasis(2, 173); !strings.Contains(got, "2 of 173 day(s)") || !strings.Contains(got, "today's balance") {
		t.Fatalf("gaps: %q", got)
	}
	s, st := snapServer(t)
	ctx := context.Background()
	if err := st.SetPreference(ctx, "cash_balance_usd", "500"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, st, `INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, cash_value_usd, computed_at) VALUES ('2026-11-12', 100, 100, 0, 480, 0)`)
	mustExec(t, st, `INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, computed_at) VALUES ('2026-11-13', 100, 100, 0, 0)`)
	chk, err := s.computeRiskCheck(ctx, 1)
	if err != nil || !strings.Contains(chk.DrawdownBasis, "1 of 2 day(s)") {
		t.Fatalf("one gap: %q %v", chk.DrawdownBasis, err)
	}
	if _, err := st.DB.Exec(`UPDATE portfolio_value_history SET cash_value_usd = 490 WHERE date = '2026-11-13'`); err != nil {
		t.Fatal(err)
	}
	if chk, _ = s.computeRiskCheck(ctx, 1); chk.DrawdownBasis != "incl. cash (recorded for every day)" {
		t.Fatalf("filled: %q", chk.DrawdownBasis)
	}
}
