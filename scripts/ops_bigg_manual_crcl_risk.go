//go:build ignore

// Widen BigG hard stop for manual CRCL position; stop bot from opening new trades.
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
	userID         = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggTraderID   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	biggStrategyID = "b723efa8-729d-47cd-a71e-99429c639b6a"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing")
		os.Exit(1)
	}
	auth.SetJWTSecret(secret)
	token, err := auth.GenerateJWT(userID, "bigg-manual-risk@local")
	if err != nil {
		panic(err)
	}
	client := &http.Client{}

	get := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			panic(fmt.Sprintf("GET %s status=%d body=%s", path, resp.StatusCode, string(body)))
		}
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return out
	}

	put := func(path string, payload map[string]any) {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, baseURL+path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		fmt.Printf("PUT %s status=%d body=%s\n", path, resp.StatusCode, string(out))
		if resp.StatusCode >= 300 {
			os.Exit(1)
		}
	}

	post := func(path string) {
		req, _ := http.NewRequest(http.MethodPost, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		fmt.Printf("POST %s status=%d body=%s\n", path, resp.StatusCode, string(out))
		if resp.StatusCode >= 300 {
			os.Exit(1)
		}
	}

	// Show current position
	posRaw, _ := http.NewRequest(http.MethodGet, baseURL+"/api/positions?trader_id="+biggTraderID, nil)
	posRaw.Header.Set("Authorization", "Bearer "+token)
	resp, _ := client.Do(posRaw)
	if resp != nil {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Printf("positions: %s\n", string(b))
	}

	trCfg := get("/api/traders/" + biggTraderID + "/config")
	fmt.Printf("trader running=%v strategy=%v\n", trCfg["is_running"], trCfg["strategy_id"])

	strategy := get("/api/strategies/" + biggStrategyID)
	config, _ := strategy["config"].(map[string]any)
	ai, _ := config["ai_config"].(map[string]any)
	risk, _ := ai["risk_control"].(map[string]any)
	fmt.Printf("before hard_stop=%v hard_tp=%v max_pos=%v\n",
		risk["hard_stop_loss_margin_pct"], risk["hard_take_profit_margin_pct"], risk["max_positions"])

	// Disable code hard stop/take for manual trade window; keep max_positions=1 so bot won't open more.
	put("/api/strategies/"+biggStrategyID, map[string]any{
		"config": map[string]any{
			"ai_config": map[string]any{
				"risk_control": map[string]any{
					"hard_stop_loss_margin_pct":   0,
					"hard_take_profit_margin_pct":   0,
					"max_positions":                 1,
					"max_margin_usage":              0.3,
				},
			},
		},
	})

	strategy = get("/api/strategies/" + biggStrategyID)
	config, _ = strategy["config"].(map[string]any)
	ai, _ = config["ai_config"].(map[string]any)
	risk, _ = ai["risk_control"].(map[string]any)
	fmt.Printf("after hard_stop=%v hard_tp=%v max_pos=%v margin=%v\n",
		risk["hard_stop_loss_margin_pct"], risk["hard_take_profit_margin_pct"],
		risk["max_positions"], risk["max_margin_usage"])

	if fmt.Sprint(trCfg["is_running"]) == "true" {
		post("/api/traders/" + biggTraderID + "/stop")
		fmt.Println("Stopped Crypto BigG — resume when manual CRCL trade is closed.")
	} else {
		fmt.Println("Crypto BigG already stopped.")
	}
}
