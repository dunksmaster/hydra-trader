//go:build ignore

// Single copy leader: close all positions, delete extra copy bots, keep one fills-only bot.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"nofx/auth"
)

const (
	userID     = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	keepLeader = "0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf"
	keepName   = "🐉 Leviathan"
	strategy   = "HL Copy Leviathan"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_single_copy_leader.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT(userID, "single-copy@local")
	client := &http.Client{Timeout: 180 * time.Second}

	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return body, resp.StatusCode
	}
	jsonReq := func(method, path string, payload any) ([]byte, int, error) {
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, baseURL+path, body)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		out, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return out, resp.StatusCode, nil
	}

	trBody, st := get("/api/my-traders")
	if st >= 300 {
		panic(fmt.Sprintf("my-traders status=%d", st))
	}
	var trList []map[string]any
	_ = json.Unmarshal(trBody, &trList)

	aiModel, exchangeID := "", ""
	var keepTraderID, keepStrategyID string
	copyTraderIDs := []string{}

	for _, tr := range trList {
		id := firstStr(tr, "trader_id", "id")
		name := fmt.Sprint(tr["trader_name"])
		sid := fmt.Sprint(tr["strategy_id"])
		if !isCopyTrader(get, sid) && !strings.Contains(strings.ToLower(name), "copy") && name != keepName {
			continue
		}
		if aiModel == "" {
			cfgBody, cfgSt := get("/api/traders/" + id + "/config")
			if cfgSt < 300 {
				var cfg map[string]any
				_ = json.Unmarshal(cfgBody, &cfg)
				aiModel = firstStr(cfg, "ai_model_id", "ai_model")
				exchangeID = fmt.Sprint(cfg["exchange_id"])
			}
		}
		if name == keepName {
			keepTraderID = id
			keepStrategyID = sid
			continue
		}
		copyTraderIDs = append(copyTraderIDs, id)
	}

	// Close every open position on Hyperliquid wallet (use any trader with exchange)
	seenClose := map[string]bool{}
	for _, tr := range trList {
		id := firstStr(tr, "trader_id", "id")
		posBody, posSt := get("/api/positions?trader_id=" + id)
		if posSt >= 300 {
			continue
		}
		var positions []map[string]any
		_ = json.Unmarshal(posBody, &positions)
		for _, p := range positions {
			sym := fmt.Sprint(p["symbol"])
			side := strings.ToUpper(fmt.Sprint(p["side"]))
			key := sym + "|" + side
			if sym == "" || side == "" || seenClose[key] {
				continue
			}
			seenClose[key] = true
			fmt.Printf("Closing %s %s\n", sym, side)
			_, _, err := jsonReq(http.MethodPost, "/api/traders/"+id+"/close-position", map[string]string{
				"symbol": sym, "side": side,
			})
			if err != nil {
				fmt.Printf("  warn: %v\n", err)
			}
			time.Sleep(2 * time.Second)
		}
	}

	// Stop and delete extra copy bots
	for _, id := range copyTraderIDs {
		fmt.Printf("Removing copy bot %s\n", id[:min(28, len(id))])
		_, _, _ = jsonReq(http.MethodPost, "/api/traders/"+id+"/stop", nil)
		time.Sleep(1 * time.Second)
		body, status, err := jsonReq(http.MethodDelete, "/api/traders/"+id, nil)
		if err != nil {
			fmt.Printf("  delete warn: %v\n", err)
		} else {
			fmt.Printf("  delete status=%d %s\n", status, string(body))
		}
	}

	copyCfg := map[string]any{
		"leader_address":         keepLeader,
		"copy_mode":              "fills",
		"size_mode":              "fixed_notional",
		"notional_usd":           50,
		"min_notional_usd":       12,
		"max_notional_pct":       55,
		"max_leverage":           10,
		"max_positions":          1,
		"wallet_copy_slots":      1,
		"exit_mode":              "leader_plus_stop",
		"safety_stop_pct":        15,
		"symbol_blocklist":       []string{},
		"reconcile_interval_sec": 60,
		"copy_on_start":          false,
		"min_leader_fill_usd":    10,
		"dry_run":                false,
		"inverse":                false,
	}

	if keepStrategyID == "" || keepStrategyID == "<nil>" {
		body, status, err := jsonReq(http.MethodPost, "/api/strategies", map[string]any{
			"name": strategy,
			"config": map[string]any{
				"strategy_type": "copy_trading",
				"language":      "en",
				"copy_config":   copyCfg,
			},
		})
		if err != nil {
			panic(err)
		}
		if status >= 300 {
			panic(string(body))
		}
		var created map[string]any
		_ = json.Unmarshal(body, &created)
		keepStrategyID = fmt.Sprint(created["id"])
	} else {
		body, status, err := jsonReq(http.MethodPut, "/api/strategies/"+keepStrategyID, map[string]any{
			"config": map[string]any{
				"strategy_type": "copy_trading",
				"language":      "en",
				"copy_config":   copyCfg,
			},
		})
		if err != nil {
			panic(err)
		}
		if status >= 300 {
			panic(string(body))
		}
		fmt.Printf("Updated strategy %s\n", keepStrategyID)
	}

	if keepTraderID == "" {
		if aiModel == "" {
			panic("need ai_model_id/exchange_id from existing trader")
		}
		body, status, err := jsonReq(http.MethodPost, "/api/traders", map[string]any{
			"name": keepName, "ai_model_id": aiModel, "exchange_id": exchangeID,
			"strategy_id": keepStrategyID, "scan_interval_minutes": 5,
			"is_cross_margin": true, "show_in_competition": false,
		})
		if err != nil {
			panic(err)
		}
		if status >= 300 {
			panic(string(body))
		}
		var created map[string]any
		_ = json.Unmarshal(body, &created)
		keepTraderID = fmt.Sprint(created["trader_id"])
	} else {
		_, _, _ = jsonReq(http.MethodPut, "/api/traders/"+keepTraderID, map[string]any{
			"name": keepName, "ai_model_id": aiModel, "exchange_id": exchangeID,
			"strategy_id": keepStrategyID, "scan_interval_minutes": 5,
			"is_cross_margin": true, "show_in_competition": false,
		})
	}

	_, _, _ = jsonReq(http.MethodPost, "/api/traders/"+keepTraderID+"/stop", nil)
	time.Sleep(3 * time.Second)
	startBody, startSt, err := jsonReq(http.MethodPost, "/api/traders/"+keepTraderID+"/start", nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Start status=%d body=%s\n", startSt, string(startBody))
	fmt.Printf("\nDone — single copy bot %s → %s (new fills only, copy_on_start=false)\n", keepName, keepLeader)
}

func isCopyTrader(get func(string) ([]byte, int), strategyID string) bool {
	if strategyID == "" || strategyID == "<nil>" {
		return false
	}
	body, st := get("/api/strategies/" + strategyID)
	if st >= 300 {
		return false
	}
	var stMap map[string]any
	if json.Unmarshal(body, &stMap) != nil {
		return false
	}
	cfg, _ := stMap["config"].(map[string]any)
	return fmt.Sprint(cfg["strategy_type"]) == "copy_trading"
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "" {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
