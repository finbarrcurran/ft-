package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// FT-039 (SC-85): the AI Nexus health alert is a system alert with holding id 0; acknowledging it must work, or it
// re-fires at every poll. Any other alert still needs a holding id.
func TestBotAckNexusSystemAlert(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	ack := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/bot/alerts/ack", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		s.handleBotAlertsAck(rec, req.WithContext(context.WithValue(context.Background(), ctxUserID, int64(1))))
		return rec.Code
	}
	today := time.Now().UTC().Format("2006-01-02")
	if got, _ := st.HasAlertBeenAckedToday(ctx, "nexus", 0, "nexus_anomaly", today); got {
		t.Fatal("not acked yet")
	}
	if c := ack(`{"holdingKind":"nexus","holdingId":0,"kind":"nexus_anomaly"}`); c != http.StatusOK {
		t.Fatalf("nexus ack: %d", c)
	}
	if got, _ := st.HasAlertBeenAckedToday(ctx, "nexus", 0, "nexus_anomaly", today); !got {
		t.Fatal("the nexus alert must now count as acknowledged today")
	}
	if c := ack(`{"holdingKind":"stock","holdingId":0,"kind":"red"}`); c != http.StatusBadRequest {
		t.Fatalf("a stock alert with no holding id must still be refused: %d", c)
	}
	if c := ack(`{"holdingKind":"stock","holdingId":12,"kind":"proximity_amber_sl"}`); c != http.StatusOK {
		t.Fatalf("a normal ack: %d", c)
	}
}
