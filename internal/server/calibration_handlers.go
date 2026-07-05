package server

import (
	"database/sql"
	"net/http"

	"ft/internal/calibration"
	"ft/internal/store"
)

// GET /api/calibration/theses — SC-38 score-vs-outcome, composed at read time
// (no persisted results table, SC-28 discipline; the response is the only
// "calibration" object and is transient). Forward, price-return-since-lock,
// SPY-excess headline. READ-ONLY w.r.t. all scoring doctrine — it never writes
// a band/weight/pillar. No fitted parameters.
func (s *Server) handleCalibrationTheses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Current locked theses only (the 2 superseded are excluded by status).
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT ticker, score, max_score, COALESCE(locked_date,'')
		  FROM theses_index WHERE status='locked'`)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	type lt struct {
		ticker     string
		score      int
		scoreNull  bool
		maxScore   int
		lockedDate string
	}
	var theses []lt
	for rows.Next() {
		var t lt
		var sc sql.NullInt64
		if err := rows.Scan(&t.ticker, &sc, &t.maxScore, &t.lockedDate); err != nil {
			rows.Close()
			mapStoreError(w, err)
			return
		}
		if sc.Valid {
			t.score = int(sc.Int64)
		} else {
			t.scoreNull = true
		}
		theses = append(theses, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		mapStoreError(w, err)
		return
	}

	// 2. SPY baseline (the excess denominator; back-filled in P1).
	spy, _ := s.store.GetDailyBars(ctx, "SPY", "stock")

	points := make([]calibration.Point, 0, len(theses))
	noOutcome := make([]calibration.NoOutcome, 0)
	for _, t := range theses {
		if t.scoreNull || t.lockedDate == "" {
			noOutcome = append(noOutcome, calibration.NoOutcome{Ticker: t.ticker, Reason: "no score or lock date"})
			continue
		}
		bars, _ := s.store.GetDailyBars(ctx, t.ticker, "stock")
		li := firstIdxAtOrAfter(bars, t.lockedDate)
		if li < 0 {
			noOutcome = append(noOutcome, calibration.NoOutcome{Ticker: t.ticker, Reason: "no price bars at/after lock date (delisted or unresolvable symbol)"})
			continue
		}
		lockClose := bars[li].Close
		if lockClose <= 0 {
			noOutcome = append(noOutcome, calibration.NoOutcome{Ticker: t.ticker, Reason: "non-positive lock-close price"})
			continue
		}
		endClose := bars[len(bars)-1].Close
		endDate := bars[len(bars)-1].Date
		holdingDays := (len(bars) - 1) - li // trading days lock -> latest (no look-ahead: li is at/after lock)
		abs := (endClose - lockClose) / lockClose

		// SPY over the IDENTICAL window: same lock-close date -> the ticker's latest date.
		var spyRet float64
		spyStart, ok1 := closeAtOrAfter(spy, bars[li].Date)
		spyEnd, ok2 := closeAtOrBefore(spy, endDate)
		if ok1 && ok2 && spyStart > 0 {
			spyRet = (spyEnd - spyStart) / spyStart
		}

		points = append(points, calibration.Point{
			Ticker:      t.ticker,
			Score:       t.score,
			MaxScore:    t.maxScore,
			LockedDate:  t.lockedDate,
			HoldingDays: holdingDays,
			AbsReturn:   abs,
			SpyReturn:   spyRet,
			Excess:      abs - spyRet,
			TooFresh:    holdingDays < 20, // ruling F
		})
	}

	summary := calibration.Summarize(points, len(noOutcome))
	writeJSON(w, http.StatusOK, map[string]any{
		"rows":          points,
		"summary":       summary,
		"noOutcomeData": noOutcome,
	})
}

// firstIdxAtOrAfter returns the index of the first bar with Date >= date (ISO,
// so lexicographic == chronological), or -1. Guarantees no look-ahead: the
// lock-close is the first bar AT OR AFTER the lock date, never before.
func firstIdxAtOrAfter(bars []store.DailyBarRow, date string) int {
	for i := range bars {
		if bars[i].Date >= date {
			return i
		}
	}
	return -1
}

func closeAtOrAfter(bars []store.DailyBarRow, date string) (float64, bool) {
	for i := range bars {
		if bars[i].Date >= date {
			return bars[i].Close, true
		}
	}
	return 0, false
}

func closeAtOrBefore(bars []store.DailyBarRow, date string) (float64, bool) {
	for i := len(bars) - 1; i >= 0; i-- {
		if bars[i].Date <= date {
			return bars[i].Close, true
		}
	}
	return 0, false
}
