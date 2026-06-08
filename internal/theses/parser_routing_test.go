package theses

import "testing"

// Covers the auto_ev + software_saas parser registrations (2026-06-08) and the
// two routing guards: automakers must not route to battery-cell / industrial_
// electrical, and EDA/Synopsys must not route to ai_infra_semi via "semi".
func TestNormaliseAdapter_NewAndRegression(t *testing.T) {
	cases := []struct{ in, want string }{
		// auto_ev (20th)
		{"Automotive", "auto_ev"},
		{"auto_ev", "auto_ev"},
		{"Auto/EV", "auto_ev"},
		{"EV & Mobility", "auto_ev"},
		{"EV", "auto_ev"},
		{"electric vehicle", "auto_ev"},
		// software_saas (21st)
		{"Software/SaaS", "software_saas"},
		{"software_saas", "software_saas"},
		{"SaaS", "software_saas"},
		{"Software", "software_saas"},
		{"EDA", "software_saas"},
		{"Design Automation", "software_saas"},
		{"Design IP", "software_saas"},
		{"Semiconductor EDA", "software_saas"}, // guard: EDA wins over "semi"
		// regression — existing adapters unchanged
		{"Pharma", "pharma"},
		{"AI-Infra/Semi", "ai_infra_semi"},
		{"Semiconductor", "ai_infra_semi"},
		{"Industrial-Electrical", "industrial_electrical"},
		{"Industrial Electrical Equipment", "industrial_electrical_equipment"},
		{"Beverages", "beverages"},
		{"Consumer Staples - Beverages", "beverages"},
		{"Utilities/IPP", "utilities_ipp"},
		{"Financials", "financials"},
		{"Asset-Hedge", "asset_hedge"},
		{"Heavy-Machinery", "heavy_machinery"},
		{"Defense", "defense"},
		{"Energy-Power", "energy_power"},
		{"Cloud-Infra", "cloud_infra"},
	}
	for _, c := range cases {
		if got := NormaliseAdapter(c.in); got != c.want {
			t.Errorf("NormaliseAdapter(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormaliseAdapter_RoutingGuards(t *testing.T) {
	for _, in := range []string{"Automotive", "auto_ev", "EV & Mobility"} {
		if got := NormaliseAdapter(in); got == "industrial_electrical" || got == "industrial_electrical_equipment" {
			t.Errorf("auto name %q mis-routed to %q (battery-cell guard)", in, got)
		}
	}
	for _, in := range []string{"EDA", "Design Automation", "Semiconductor EDA", "Software/SaaS"} {
		if got := NormaliseAdapter(in); got == "ai_infra_semi" {
			t.Errorf("EDA/software name %q mis-routed to ai_infra_semi", in)
		}
	}
}
