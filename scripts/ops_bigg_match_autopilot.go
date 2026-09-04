//go:build ignore

// Copy NOFX Autopilot strategy onto Crypto BigG (Bitget) and use the full margin book.
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
	userID       = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggTrader   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	biggStrategy = "e6e58a0f-5b1a-4a28-a472-9c6743311db4"
	apStrategy   = "2e50a1e7-cb16-4d0f-8ed7-c8ea6cda3ad3"
	base         = "https://nofx-production-fcd1.up.railway.app"
)

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fatal("JWT_SECRET missing")
	}
	auth.SetJWTSecret(secret)
	token, err := auth.GenerateJWT(userID, "ops-bigg-sync@local")
	if err != nil {
		fatal("jwt: %v", err)
	}
	c := &client{token: token}

	ap, _ := c.getJSON("/api/strategies/" + apStrategy).(map[string]any)
	apCfg, _ := ap["config"].(map[string]any)
	apAI, _ := apCfg["ai_config"].(map[string]any)
	if apAI == nil {
		fatal("autopilot ai_config missing")
	}

	// Mirror Autopilot exactly — same book, same hard exits, same confidence.
	risk, _ := apAI["risk_control"].(map[string]any)
	// risk map is reused from apAI; values already match Autopilot.

	prompts, _ := apAI["prompt_sections"].(map[string]any)
	customPrompt := "Run Crypto BigG on Bitget with the same Hyperliquid crypto Autopilot strategy: volume-ranked core perps (BTC, ETH, SOL, HYPE). Same sizing, confidence, and hard exits as Autopilot. Confirm timing with OHLCV."

	patch := map[string]any{
		"config": map[string]any{
			"ai_config": map[string]any{
				"coin_source":      apAI["coin_source"],
				"indicators":       apAI["indicators"],
				"risk_control":     risk,
				"prompt_sections":  prompts,
				"decision_process": apAI["decision_process"],
				"role_definition":  apAI["role_definition"],
				"custom_prompt":    customPrompt,
			},
		},
	}

	st, body := c.put("/api/strategies/"+biggStrategy, patch)
	fmt.Printf("bigg strategy patch status=%d body=%s\n", st, trim(body))
	if st >= 300 {
		os.Exit(1)
	}

	cfg, _ := c.getJSON("/api/traders/" + biggTrader + "/config").(map[string]any)
	payload := map[string]any{
		"name":                  cfg["trader_name"],
		"ai_model_id":           cfg["ai_model"],
		"exchange_id":           cfg["exchange_id"],
		"strategy_id":           biggStrategy,
		"scan_interval_minutes": 10,
	}
	if v, ok := cfg["is_cross_margin"].(bool); ok {
		payload["is_cross_margin"] = v
	}
	if v, ok := cfg["initial_balance"]; ok {
		payload["initial_balance"] = v
	}
	st, body = c.put("/api/traders/"+biggTrader, payload)
	fmt.Printf("bigg scan -> 10m status=%d body=%s\n", st, trim(body))

	after, _ := c.getJSON("/api/strategies/" + biggStrategy).(map[string]any)
	acfg, _ := after["config"].(map[string]any)
	aai, _ := acfg["ai_config"].(map[string]any)
	fmt.Printf("\nafter risk=%s\n", must(aai["risk_control"]))
	tr, _ := c.getJSON("/api/traders/" + biggTrader + "/config").(map[string]any)
	fmt.Printf("after scan_min=%v running=%v\n", tr["scan_interval_minutes"], tr["is_running"])

	eq := c.getJSON("/api/account?trader_id=" + biggTrader)
	fmt.Printf("account=%s\n", must(eq))
}

type client struct{ token string }

func (c *client) getJSON(path string) any {
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		fatal("GET %s status=%d %s", path, resp.StatusCode, string(b))
	}
	var out any
	json.Unmarshal(b, &out)
	return out
}

func (c *client) put(path string, payload any) (int, []byte) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func must(v any) string { b, _ := json.Marshal(v); return string(b) }
func trim(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}
func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
