//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"nofx/auth"
)

const (
	userID           = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	autopilotTrader  = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502"
	fallbackStrategy = "2e50a1e7"
)

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fatal("JWT_SECRET missing")
	}
	auth.SetJWTSecret(secret)
	token, err := auth.GenerateJWT(userID, "ops-ap-fast@local")
	if err != nil {
		fatal("jwt: %v", err)
	}
	c := &client{base: "https://nofx-production-fcd1.up.railway.app", token: token, h: http.DefaultClient}

	sid := findStrategy(c)
	if sid == "" {
		sid = fallbackStrategy
	}
	fmt.Printf("autopilot strategy=%s\n", sid)

	patch := map[string]any{
		"config": map[string]any{
			"ai_config": map[string]any{
				"risk_control": map[string]any{
					"max_positions":                    2,
					"min_confidence":                   65,
					"btc_eth_max_leverage":             10,
					"altcoin_max_leverage":             10,
					"btc_eth_max_position_value_ratio": 5,
					"altcoin_max_position_value_ratio": 5,
					"max_margin_usage":                 1,
					"min_position_size":                12,
					"min_risk_reward_ratio":            1.5,
					"hard_take_profit_margin_pct":      10,
				},
				"prompt_sections": map[string]any{
					"trading_frequency": `# Fast dollar book (Hyperliquid Autopilot)

Use the full allowed notional. Idle cash is a wasted slot. Target realized P&L of about $2, $4, $5, $8 or $10 per closed trade, and about 8–12 closes per day when the tape allows.

Code will also force-close around +10% margin (~+1% price at 10x), which is about $2 on a full-size $47 book. Let winners run toward +2–4% price ($5–$10) when the move is clean. Do not hold 20 hours for a 50-cent scalp.

Hold at least 20 minutes unless price is already ≤ -1.2% or ≥ +1.2%. After a close, the same symbol can re-enter after 30 minutes.`,
					"entry_standards": `# Entry Standards

Open when 15m and 1h agree. Prefer BTC/ETH/SOL/HYPE from the volume board. Size is set in code to equity × 5 notional (full 10x book). Do not output tiny position_size_usd.`,
				},
			},
		},
	}
	st, body := c.put("/api/strategies/"+sid, patch)
	fmt.Printf("patch status=%d body=%s\n", st, string(body))
	if st >= 300 {
		os.Exit(1)
	}

	cfgRaw := c.getJSON("/api/traders/" + autopilotTrader + "/config")
	traderCfg, _ := cfgRaw.(map[string]any)
	scanPayload := map[string]any{
		"name":                  traderCfg["trader_name"],
		"ai_model_id":           traderCfg["ai_model"],
		"exchange_id":           traderCfg["exchange_id"],
		"strategy_id":           sid,
		"scan_interval_minutes": 10,
	}
	if v, ok := traderCfg["is_cross_margin"].(bool); ok {
		scanPayload["is_cross_margin"] = v
	}
	if v, ok := traderCfg["initial_balance"]; ok {
		scanPayload["initial_balance"] = v
	}
	st, body = c.put("/api/traders/"+autopilotTrader, scanPayload)
	fmt.Printf("scan status=%d body=%s\n", st, string(body))
	if st >= 300 {
		os.Exit(1)
	}

	strategy := c.getJSON("/api/strategies/" + sid).(map[string]any)
	cfg, _ := strategy["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	risk, _ := ai["risk_control"].(map[string]any)
	fmt.Printf("after risk=%s\n", must(risk))

	after := c.getJSON("/api/traders/" + autopilotTrader + "/config").(map[string]any)
	fmt.Printf("after scan_min=%v running=%v strategy=%v\n",
		after["scan_interval_minutes"], after["is_running"], after["strategy_id"])
}

func findStrategy(c *client) string {
	raw := c.getJSON("/api/my-traders")
	list, _ := raw.([]any)
	for _, item := range list {
		t, _ := item.(map[string]any)
		if t["trader_id"] == autopilotTrader || t["trader_name"] == "NOFX Autopilot" || t["name"] == "NOFX Autopilot" {
			if sid, _ := t["strategy_id"].(string); sid != "" {
				return sid
			}
		}
	}
	return ""
}

type client struct {
	base, token string
	h           *http.Client
}

func (c *client) getJSON(path string) any {
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.h.Do(req)
	if err != nil {
		fatal("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		fatal("GET %s status=%d body=%s", path, resp.StatusCode, string(b))
	}
	var out any
	json.Unmarshal(b, &out)
	return out
}

func (c *client) put(path string, payload any) (int, []byte) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, c.base+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.h.Do(req)
	if err != nil {
		fatal("PUT: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func must(v any) string { b, _ := json.Marshal(v); return string(b) }
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
