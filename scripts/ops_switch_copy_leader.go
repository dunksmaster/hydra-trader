//go:build ignore

// Switch Autopilot Copy to a new Hyperliquid leader and restart (fills mode).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"nofx/auth"
	hlprovider "nofx/provider/hyperliquid"
)

const (
	userID       = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	copyTraderID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468"
	strategyID   = "00e95f8a-baf4-4d80-85fb-9ce5060e7fbb"
	// Moderate daily trader: ~4 trades/day, 66.6% WR, PF 2.91, $322k equity (Aug 2026 screen)
	newLeader = "0x335f45392f8d87745aaae68f5c192849afd9b60e"
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
	token, _ := auth.GenerateJWT(userID, "copy-switch@local")
	client := &http.Client{Timeout: 30 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	leader, err := hlprovider.FetchAccountStateAll(ctx, newLeader)
	if err != nil {
		panic(fmt.Sprintf("leader fetch: %v", err))
	}
	fmt.Printf("NEW LEADER %s equity=$%.2f open_legs=%d\n", short(newLeader), leader.Equity, len(leader.Legs))
	sort.Slice(leader.Legs, func(i, j int) bool {
		return leader.Legs[i].NotionalUSD > leader.Legs[j].NotionalUSD
	})
	for i, leg := range leader.Legs {
		if i >= 5 {
			break
		}
		fmt.Printf("  leg: %s %s notional=$%.0f lev=%dx\n", leg.Symbol, leg.Side, leg.NotionalUSD, leg.Leverage)
	}

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

	acct := get("/api/account?trader_id=" + copyTraderID)
	equity, _ := acct["total_equity"].(float64)
	avail, _ := acct["available_balance"].(float64)
	fmt.Printf("COPY BOT equity=$%.2f available=$%.2f positions=%v\n", equity, avail, acct["position_count"])

	notional := 15.0
	if avail > 80 {
		notional = 25
	} else if avail > 50 {
		notional = 20
	}

	payload := map[string]any{
		"config": map[string]any{
			"strategy_type": "copy_trading",
			"language":      "en",
			"copy_config": map[string]any{
				"leader_address":         newLeader,
				"copy_mode":              "fills",
				"size_mode":              "fixed_notional",
				"notional_usd":           notional,
				"min_notional_usd":       12,
				"max_notional_pct":       45,
				"max_leverage":           10,
				"max_positions":          3,
				"exit_mode":              "leader_plus_stop",
				"safety_stop_pct":        15,
				"symbol_blocklist":       []string{},
				"reconcile_interval_sec": 60,
				"copy_on_start":          true,
				"min_leader_fill_usd":    10,
				"dry_run":                false,
				"inverse":                false,
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
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("strategy update status=%d body=%s\n", resp.StatusCode, string(out))
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}

	for _, step := range []struct{ method, path string }{
		{http.MethodPost, "/api/traders/" + copyTraderID + "/stop"},
		{http.MethodPost, "/api/traders/" + copyTraderID + "/start"},
	} {
		req, _ := http.NewRequest(step.method, baseURL+step.path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%s %s status=%d body=%s\n", step.method, step.path, resp.StatusCode, string(b))
	}

	st := get("/api/strategies/" + strategyID)
	cfg, _ := st["config"].(map[string]any)
	copyCfg, _ := cfg["copy_config"].(map[string]any)
	fmt.Printf("verified leader=%v copy_mode=%v max_positions=%v\n",
		copyCfg["leader_address"], copyCfg["copy_mode"], copyCfg["max_positions"])
	cfgTrader := get("/api/traders/" + copyTraderID + "/config")
	fmt.Printf("copy trader running=%v\n", cfgTrader["is_running"])
}

func short(a string) string {
	if len(a) <= 12 {
		return a
	}
	return a[:6] + "..." + a[len(a)-4:]
}
