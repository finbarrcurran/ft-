package videodigest

import "testing"

func TestParseFrameReadsSalvage(t *testing.T) {
	full := "```json\n[{\"t_sec\":10,\"series_or_indicator\":\"BTC risk\",\"visible_values\":\"0.62\",\"confidence\":\"high\"},{\"t_sec\":40,\"series_or_indicator\":\"MVRV\",\"visible_values\":null,\"confidence\":\"low\"}]\n```"
	if got := parseFrameReads(full); len(got) != 2 || got[0].Series != "BTC risk" {
		t.Fatalf("full parse: %+v", got)
	}
	cut := `[{"t_sec":10,"series_or_indicator":"S&P {weekly}","visible_values":"4,100 \"high\"","confidence":"high"},{"t_sec":40,"series_or_indicator":"MV`
	got := parseFrameReads(cut)
	if len(got) != 1 || got[0].Series != "S&P {weekly}" || valuesText(got[0].Values) != `4,100 "high"` {
		t.Fatalf("salvage: %+v", got)
	}
	if got := parseFrameReads("no json here"); len(got) != 0 {
		t.Fatalf("garbage: %+v", got)
	}
}
