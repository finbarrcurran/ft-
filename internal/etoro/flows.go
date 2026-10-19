package etoro

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

// SC-52 P4 — dated capital flows from the Account Activity sheet, reconciled to the Account
// Summary totals of the same statement.

const sheetActivity = "Account Activity"

// CapitalFlow is one deposit / withdrawal / transfer row.
type CapitalFlow struct {
	Key        string  `json:"key"`
	OccurredAt string  `json:"occurredAt"` // YYYY-MM-DDTHH:MM:SS
	Day        string  `json:"day"`
	Kind       string  `json:"kind"` // deposit | transfer_in | withdrawal | transfer_out | reversal | fx_fee
	AmountUSD  float64 `json:"amountUsd"`
	Details    string  `json:"details"`
}

// FlowSummary is the statement's own Account Summary view of the same flows, next to what the
// Activity rows add up to. Inflows = Deposits + Transfer to Trading; Outflows(transfers) =
// Transfer to eToro Money (which, by the numbers, includes the cancelled-request reversals).
type FlowSummary struct {
	Present bool `json:"present"`
	// Account Summary lines.
	SumDeposits, SumTransferToTrading, SumWithdrawals, SumTransferOut, SumFXFees float64
	// Activity rows.
	RowDeposits, RowTransfersIn, RowWithdrawals, RowTransfersOut, RowReversals, RowFXFees float64
	Reconciled                                                                            bool
}

var flowTypes = map[string]string{
	"Deposit":                    "deposit",
	"Transfer: EUR > USD":        "transfer_in",
	"Withdraw Request":           "withdrawal",
	"Transfer: USD > EUR":        "transfer_out",
	"Withdraw Request Cancelled": "reversal",
	"Deposit Conversion Fee":     "fx_fee",
	"Withdrawal Conversion Fee":  "fx_fee",
	"Withdrawal Fee":             "fx_fee",
}

var flowLike = regexp.MustCompile(`(?i)deposit|withdraw|transfer|credit`)

func actDate(s string) (day, at string) {
	s = strings.TrimSpace(s)
	day = isoFromDDMMYYYY(s)
	if day == "" {
		return "", ""
	}
	clock := "00:00:00"
	if len(s) >= 19 {
		clock = s[11:19]
	}
	return day, day + "T" + clock
}

// parseFlows reads the Account Activity sheet and the Account Summary flow lines. A statement
// without the sheet yields no flows and Present=false (nothing is invented).
func parseFlows(f *excelize.File, have map[string]bool) ([]CapitalFlow, FlowSummary, []string) {
	var fs FlowSummary
	var warns []string
	if !have[sheetActivity] {
		return nil, fs, nil
	}
	rows, err := f.GetRows(sheetActivity)
	if err != nil || len(rows) < 2 {
		return nil, fs, nil
	}
	idx := headerIndex(rows[0])
	iDate, iType, iDet, iAmt := idx["Date"], idx["Type"], idx["Details"], idx["Amount"]
	if _, ok := idx["Type"]; !ok {
		return nil, fs, []string{"Account Activity sheet has no Type column; capital flows were not read"}
	}
	var out []CapitalFlow
	seen := map[string]int{}
	unknown := map[string]int{}
	for _, r := range rows[1:] {
		typ := cell(r, iType)
		kind, ok := flowTypes[typ]
		if !ok {
			if flowLike.MatchString(typ) {
				unknown[typ]++
			}
			continue
		}
		day, at := actDate(cell(r, iDate))
		if day == "" {
			continue
		}
		amt := parseNum(cell(r, iAmt))
		det := cell(r, iDet)
		base := fmt.Sprintf("%s|%s|%s|%.4f", at, typ, det, amt)
		seen[base]++
		h := sha1.Sum([]byte(fmt.Sprintf("%s|%d", base, seen[base])))
		out = append(out, CapitalFlow{Key: hex.EncodeToString(h[:10]), OccurredAt: at, Day: day, Kind: kind, AmountUSD: amt, Details: det})
		switch kind {
		case "deposit":
			fs.RowDeposits += amt
		case "transfer_in":
			fs.RowTransfersIn += amt
		case "withdrawal":
			fs.RowWithdrawals += amt
		case "transfer_out":
			fs.RowTransfersOut += amt
		case "reversal":
			fs.RowReversals += amt
		case "fx_fee":
			fs.RowFXFees += amt
		}
	}
	if len(unknown) > 0 {
		names := make([]string, 0, len(unknown))
		for k, n := range unknown {
			names = append(names, fmt.Sprintf("%s (%d)", k, n))
		}
		sort.Strings(names)
		warns = append(warns, "unrecognised flow-like activity types, not stored: "+strings.Join(names, ", "))
	}

	// Account Summary lines.
	sum, _ := f.GetRows(sheetAccountSummary)
	lines := map[string]float64{}
	for _, r := range sum {
		if len(r) >= 2 {
			lines[strings.TrimSpace(r[0])] = parseNum(cell(r, 1))
		}
	}
	if _, ok := lines["Deposits"]; !ok {
		fs.Present = false
		return out, fs, warns
	}
	fs.Present = true
	fs.SumDeposits = lines["Deposits"]
	fs.SumTransferToTrading = lines["Transfer to Trading (from eToro Money)"]
	fs.SumWithdrawals = lines["Withdrawals"]
	fs.SumTransferOut = lines["Transfer to eToro Money (from Trading)"]
	fs.SumFXFees = lines["Deposit/Withdrawal FX Conversion Fee"] + lines["Withdrawal Fees"]
	for _, k := range []string{"Internal Transfer", "Credits", "Transfer in", "Transfer out"} {
		if v := lines[k]; math.Abs(v) > 0.004 {
			warns = append(warns, fmt.Sprintf("Account Summary line %q is %.2f and is not classified as a capital flow", k, v))
		}
	}
	const tol = 0.015
	diff := func(name string, summary, rows float64) bool {
		if math.Abs(summary-rows) > tol {
			warns = append(warns, fmt.Sprintf("capital flows do not reconcile on %s: Account Summary %.2f vs Activity rows %.2f (delta %.2f)", name, summary, rows, summary-rows))
			return false
		}
		return true
	}
	ok1 := diff("inflows (deposits + transfers to trading)", fs.SumDeposits+fs.SumTransferToTrading, fs.RowDeposits+fs.RowTransfersIn)
	ok2 := diff("withdrawals", fs.SumWithdrawals, fs.RowWithdrawals)
	ok3 := diff("transfers to eToro Money (net of reversals)", fs.SumTransferOut, fs.RowTransfersOut+fs.RowReversals)
	// eToro rounds each fee row to the cent, so the rows can differ from the total by a few cents.
	ok4 := true
	if math.Abs(fs.SumFXFees-fs.RowFXFees) > 0.05 {
		warns = append(warns, fmt.Sprintf("capital flows do not reconcile on FX conversion fees: Account Summary %.2f vs Activity rows %.2f (delta %.2f)", fs.SumFXFees, fs.RowFXFees, fs.SumFXFees-fs.RowFXFees))
		ok4 = false
	}
	fs.Reconciled = ok1 && ok2 && ok3 && ok4
	return out, fs, warns
}
