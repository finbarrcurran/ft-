package cryptoindicators

// Layman tooltips per Spec 9e §D9. Source of truth for the [info] icon
// hover text on each indicator card. Plain English; explains what
// triggers bullish / bearish; gives one concrete number to anchor.

var tooltips = map[string]string{
	"cowen_price_vs_200wma": "BTC's long-term centre of gravity. Below = generational discount (accumulation zone). Above = uptrend intact. Trigger: < 1.0 = bullish, > 2.0 = late-cycle caution.",
	"cowen_log_band":        "Where BTC sits in its 12-year growth curve. Lower third = cheap historically. Upper third = expensive historically. Trigger: lower = bullish, upper = bearish.",
	"cowen_risk_indicator":  "Cowen's composite 0-1 risk score. < 0.25 = lean in. > 0.75 = scale out. Proxy computed locally; manual entry overrides when fresh.",
	"cowen_btc_dominance":   "BTC's share of total crypto market cap. Rising = capital concentrating in BTC (defensive within crypto). Falling = alts rotating (risk-on within crypto).",
	"cowen_eth_btc":         "Risk-on vs risk-off within crypto. Falling/basing = BTC-favoured (good for BTC-only buys). Rising = late-cycle alt rotation.",
	"pal_dxy":               "USD strength. Strong dollar drains risk assets globally. Trigger: falling DXY, ideally < 100 = bullish. Rising > 105 = bearish.",
	"pal_us2y":              "Market's read on Fed policy path. Falling 2Y = cut expectations = risk-on supportive. Rising fast = hike fear / hot inflation.",
	"pal_ism":               "Business cycle pulse. > 50 = expansion, < 50 = contraction. Pal's most underrated single indicator. Trigger: > 50 and rising = bullish; < 50 falling = recession risk.",
	"universal_etf_flow_7d": "Institutional demand pulse. Biggest single variable post-Jan 2024. Trigger: 7d net > +$1B = strongly bullish; sustained outflows = bearish.",
	"universal_stablecoin_supply": "Dry powder waiting inside crypto. Rising = mint cycle (liquidity coming). Contracting = redemptions (liquidity leaving).",
	"sentiment_fear_greed":  "Crowd emotion, contrarian indicator. < 25 (Extreme Fear) = contrarian buy. > 75 (Extreme Greed) = contrarian caution.",
}

// TooltipFor returns the layman explainer for an indicator id. Empty
// string if unknown — caller renders nothing rather than an error UI.
func TooltipFor(id string) string {
	return tooltips[id]
}

// BucketLabel maps bucket slug to a human label for section headers.
func BucketLabel(b string) string {
	switch b {
	case "cowen":
		return "Cowen — cycle position"
	case "pal":
		return "Pal — macro liquidity"
	case "universal":
		return "Universal — market structure"
	case "sentiment":
		return "Sentiment"
	}
	return b
}

// BandLabel maps action_band slug to a UI label.
func BandLabel(b string) string {
	switch b {
	case "strong_accumulate":
		return "STRONG ACCUMULATE"
	case "accumulate":
		return "ACCUMULATE"
	case "neutral":
		return "NEUTRAL"
	case "caution":
		return "CAUTION"
	case "distribute_wait":
		return "DISTRIBUTE / WAIT"
	}
	return b
}
