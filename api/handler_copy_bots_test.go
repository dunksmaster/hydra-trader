package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nofx/store"

	"github.com/gin-gonic/gin"
)

func TestStrategyMetaForTraderCopyLayer(t *testing.T) {
	st, err := store.New(t.TempDir() + "/nofx.db")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	userID := "user-copy-test"
	strategyID := "strat-copy-1"
	cfg := store.StrategyConfig{
		StrategyType: "copy_trading",
		CopyConfig: &store.CopyStrategyConfig{
			LeaderAddress: "0xabc123",
			CopyLayer:     3,
			CopyPaused:    true,
		},
	}
	raw, _ := json.Marshal(cfg)
	if err := st.Strategy().Create(&store.Strategy{
		ID:     strategyID,
		UserID: userID,
		Name:   "Copy Test",
		Config: string(raw),
	}); err != nil {
		t.Fatalf("create strategy: %v", err)
	}

	s := &Server{store: st}
	stype, layer, paused := strategyMetaForTrader(s, userID, strategyID)
	if stype != "copy_trading" || layer != 3 || !paused {
		t.Fatalf("meta = %q %d %v", stype, layer, paused)
	}
}

func TestHandleCopyBotsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(t.TempDir() + "/nofx.db")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	s := &Server{store: st}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "empty-user")
	s.handleCopyBots(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if out["profile"] != "current" {
		t.Fatalf("profile = %v", out["profile"])
	}
	bots, _ := out["bots"].([]any)
	if len(bots) != 0 {
		t.Fatalf("expected 0 bots, got %d", len(bots))
	}
}
