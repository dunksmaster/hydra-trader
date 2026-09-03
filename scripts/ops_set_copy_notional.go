//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"nofx/auth"
)

const (
	userID       = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	copyTraderID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468"
	strategyID   = "00e95f8a-baf4-4d80-85fb-9ce5060e7fbb"
	newNotional  = 50.0
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT(userID, "notional-50@local")
	client := &http.Client{Timeout: 60 * time.Second}

	get := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			panic(fmt.Sprintf("GET %s status=%d body=%s", path, resp.StatusCode, string(body)))
		}
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return out
	}

	st := get("/api/strategies/" + strategyID)
	cfg, _ := st["config"].(map[string]any)
	copyCfg, _ := cfg["copy_config"].(map[string]any)
	leader := fmt.Sprint(copyCfg["leader_address"])

	acct := get("/api/account?trader_id=" + copyTraderID)
	equity, _ := acct["total_equity"].(float64)
	avail, _ := acct["available_balance"].(float64)
	posCount, _ := acct["position_count"].(float64)
	fmt.Printf("BEFORE equity=$%.2f available=$%.2f positions=%.0f notional=$%v max_pos=%v\n",
		equity, avail, posCount, copyCfg["notional_usd"], copyCfg["max_positions"])

	// 3 x $50 notional @ 10x lev ≈ $15 margin each → ~$45 total margin for 3 legs
	margin3 := newNotional * 3 / 10.0
	fmt.Printf("Estimated margin for 3x$50 @10x lev: ~$%.0f (need available >= this)\n", margin3)

	payload := map[string]any{
		"config": map[string]any{
			"strategy_type": "copy_trading",
			"language":      cfg["language"],
			"copy_config": map[string]any{
				"leader_address":         leader,
				"copy_mode":              copyCfg["copy_mode"],
				"size_mode":              "fixed_notional",
				"notional_usd":           newNotional,
				"min_notional_usd":       12,
				"max_notional_pct":       55,
				"max_leverage":           copyCfg["max_leverage"],
				"max_positions":          1,
				"wallet_copy_slots":      3,
				"exit_mode":              copyCfg["exit_mode"],
				"safety_stop_pct":        copyCfg["safety_stop_pct"],
				"symbol_blocklist":       copyCfg["symbol_blocklist"],
				"reconcile_interval_sec": copyCfg["reconcile_interval_sec"],
				"copy_on_start":          copyCfg["copy_on_start"],
				"min_leader_fill_usd":    copyCfg["min_leader_fill_usd"],
				"dry_run":                copyCfg["dry_run"],
				"inverse":                copyCfg["inverse"],
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/strategies/"+strategyID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("strategy update status=%d body=%s\n", resp.StatusCode, string(out))
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}

	// Reload running trader config
	req2, _ := http.NewRequest(http.MethodPost, baseURL+"/api/traders/"+copyTraderID+"/stop", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	client.Do(req2)
	time.Sleep(2 * time.Second)
	req3, _ := http.NewRequest(http.MethodPost, baseURL+"/api/traders/"+copyTraderID+"/start", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	resp3, _ := client.Do(req3)
	b3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	fmt.Printf("restart status=%d body=%s\n", resp3.StatusCode, string(b3))

	verify := get("/api/strategies/" + strategyID)
	vCfg, _ := verify["config"].(map[string]any)
	vCopy, _ := vCfg["copy_config"].(map[string]any)
	fmt.Printf("AFTER notional_usd=%v max_positions=%v leader=%v\n",
		vCopy["notional_usd"], vCopy["max_positions"], vCopy["leader_address"])
}
