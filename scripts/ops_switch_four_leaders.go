//go:build ignore

// Switch to 4 named copy leaders; stop orphans, close positions, restart fills-mode bots.
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

const userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"

type botSpec struct {
	StrategyName string
	TraderName   string
	Leader       string
}

var leaders = []botSpec{
	{StrategyName: "HL Copy Leviathan", TraderName: "🐉 Leviathan", Leader: "0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf"},
	{StrategyName: "HL Copy Grinder", TraderName: "Grinder", Leader: "0xdebbea84972174f44778a00521b1b5faa663abbb"},
	{StrategyName: "HL Copy Money Printer", TraderName: "Money Printer", Leader: "0x8a0cd16a004e21e04936a0a01c6f5a49ff937914"},
	{StrategyName: "HL Copy L4", TraderName: "Copy L4", Leader: "0x6a02aedceac5a6813d960e4dae1910d9c458e77c"},
}

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run via: railway run -- go run ./scripts/ops_switch_four_leaders.go")
		os.Exit(1)
	}
	token, _ := auth.GenerateJWT(userID, "four-leaders@local")
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

	// Template exchange/model from first copy trader
	trBody0, trStatus0 := get("/api/my-traders")
	if trStatus0 >= 300 {
		panic(fmt.Sprintf("GET my-traders status=%d body=%s", trStatus0, string(trBody0)))
	}
	var trList0 []map[string]any
	_ = json.Unmarshal(trBody0, &trList0)
	aiModelID, exchangeID := "", ""
	for _, tr := range trList0 {
		name := strings.ToLower(fmt.Sprint(tr["trader_name"]))
		if !strings.Contains(name, "copy") && !strings.Contains(name, "autopilot") {
			continue
		}
		id := firstString(tr, "trader_id", "id")
		cfgBody, cfgStatus := get("/api/traders/" + id + "/config")
		if cfgStatus >= 300 {
			continue
		}
		var cfg map[string]any
		_ = json.Unmarshal(cfgBody, &cfg)
		aiModelID = firstString(cfg, "ai_model_id", "ai_model")
		exchangeID = fmt.Sprint(cfg["exchange_id"])
		if aiModelID != "" && exchangeID != "" && exchangeID != "<nil>" {
			break
		}
	}
	if aiModelID == "" || exchangeID == "" || exchangeID == "<nil>" {
		panic("could not resolve ai_model_id/exchange_id from existing copy trader")
	}
	fmt.Printf("using model=%s exchange=%s\n", aiModelID, exchangeID)

	// List traders
	trBody, _ := get("/api/my-traders")
	var trList []map[string]any
	_ = json.Unmarshal(trBody, &trList)
	if len(trList) == 0 {
		var wrap map[string]any
		_ = json.Unmarshal(trBody, &wrap)
		if items, ok := wrap["traders"].([]any); ok {
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					trList = append(trList, m)
				}
			}
		}
	}

	wantNames := map[string]bool{}
	for _, spec := range leaders {
		wantNames[spec.TraderName] = true
	}

	// Stop all copy traders and close positions
	seenTrader := map[string]bool{}
	for _, tr := range trList {
		name := fmt.Sprint(tr["trader_name"], tr["name"])
		if !strings.Contains(strings.ToLower(name), "copy") && !strings.Contains(strings.ToLower(name), "autopilot") {
			continue
		}
		id := firstString(tr, "trader_id", "id")
		if id == "" || seenTrader[id] {
			continue
		}
		seenTrader[id] = true
		fmt.Printf("Stopping %s (%s)\n", name, id[:min(28, len(id))])
		if _, _, err := jsonReq(http.MethodPost, "/api/traders/"+id+"/stop", nil); err != nil {
			fmt.Printf("  stop warn: %v\n", err)
		}
		time.Sleep(2 * time.Second)

		posBody, posStatus := get("/api/positions?trader_id=" + id)
		if posStatus < 300 {
			var positions []map[string]any
			_ = json.Unmarshal(posBody, &positions)
			for _, p := range positions {
				sym := fmt.Sprint(p["symbol"])
				side := strings.ToUpper(fmt.Sprint(p["side"]))
				if sym == "" || side == "" {
					continue
				}
				fmt.Printf("  closing %s %s\n", sym, side)
				if _, _, err := jsonReq(http.MethodPost, "/api/traders/"+id+"/close-position", map[string]string{
					"symbol": sym,
					"side":   side,
				}); err != nil {
					fmt.Printf("  close warn: %v\n", err)
				}
				time.Sleep(2 * time.Second)
			}
		}
	}

	// Delete orphan copy traders not in our target list (duplicates, old Autopilot Copy name)
	for _, tr := range trList {
		name := strings.TrimSpace(fmt.Sprint(tr["trader_name"]))
		if !strings.Contains(strings.ToLower(name), "copy") && !strings.Contains(strings.ToLower(name), "autopilot copy") {
			continue
		}
		if wantNames[name] {
			continue
		}
		id := firstString(tr, "trader_id", "id")
		fmt.Printf("Deleting orphan %s (%s)\n", name, id[:min(28, len(id))])
		body, status, err := jsonReq(http.MethodDelete, "/api/traders/"+id, nil)
		if err != nil {
			fmt.Printf("  delete warn: %v\n", err)
			continue
		}
		fmt.Printf("  delete status=%d body=%s\n", status, string(body))
	}

	// Strategies index
	stBody, stStatus := get("/api/strategies")
	if stStatus >= 300 {
		panic(fmt.Sprintf("GET strategies status=%d", stStatus))
	}
	strategyByName := map[string]string{}
	var stArr []map[string]any
	if json.Unmarshal(stBody, &stArr) != nil {
		var wrap struct {
			Strategies []map[string]any `json:"strategies"`
		}
		if err := json.Unmarshal(stBody, &wrap); err != nil {
			panic(err)
		}
		stArr = wrap.Strategies
	}
	for _, st := range stArr {
		strategyByName[fmt.Sprint(st["name"])] = fmt.Sprint(st["id"])
	}

	trBody2, _ := get("/api/my-traders")
	trList = nil
	_ = json.Unmarshal(trBody2, &trList)
	traderByName := map[string]string{}
	for _, tr := range trList {
		name := fmt.Sprint(tr["trader_name"])
		traderByName[name] = firstString(tr, "trader_id", "id")
	}

	copyConfig := func(leader string) map[string]any {
		return map[string]any{
			"leader_address":         leader,
			"copy_mode":              "fills",
			"size_mode":              "fixed_notional",
			"notional_usd":           50,
			"min_notional_usd":       12,
			"max_notional_pct":       55,
			"max_leverage":           10,
			"max_positions":          1,
			"wallet_copy_slots":      4,
			"exit_mode":              "leader_plus_stop",
			"safety_stop_pct":        15,
			"symbol_blocklist":       []string{},
			"reconcile_interval_sec": 60,
			"copy_on_start":          false,
			"min_leader_fill_usd":    10,
			"dry_run":                false,
			"inverse":                false,
		}
	}

	for _, spec := range leaders {
		fmt.Printf("\n=== %s → %s ===\n", spec.TraderName, shortAddr(spec.Leader))
		strategyID := strategyByName[spec.StrategyName]
		if strategyID == "" || strategyID == "<nil>" {
			body, status, err := jsonReq(http.MethodPost, "/api/strategies", map[string]any{
				"name":        spec.StrategyName,
				"description": "Copy " + shortAddr(spec.Leader),
				"config": map[string]any{
					"strategy_type": "copy_trading",
					"language":      "en",
					"copy_config":   copyConfig(spec.Leader),
				},
			})
			if err != nil {
				panic(err)
			}
			fmt.Printf("create strategy status=%d\n", status)
			if status >= 300 {
				panic(string(body))
			}
			var created map[string]any
			_ = json.Unmarshal(body, &created)
			strategyID = fmt.Sprint(created["id"])
		} else {
			body, status, err := jsonReq(http.MethodPut, "/api/strategies/"+strategyID, map[string]any{
				"config": map[string]any{
					"strategy_type": "copy_trading",
					"language":      "en",
					"copy_config":   copyConfig(spec.Leader),
				},
			})
			if err != nil {
				panic(err)
			}
			fmt.Printf("update strategy status=%d\n", status)
			if status >= 300 {
				panic(string(body))
			}
		}

		traderID := traderByName[spec.TraderName]
		payload := map[string]any{
			"name":                  spec.TraderName,
			"ai_model_id":           aiModelID,
			"exchange_id":           exchangeID,
			"strategy_id":           strategyID,
			"scan_interval_minutes": 5,
			"is_cross_margin":       true,
			"show_in_competition":   false,
		}
		if traderID == "" {
			body, status, err := jsonReq(http.MethodPost, "/api/traders", payload)
			if err != nil {
				panic(err)
			}
			fmt.Printf("create trader status=%d body=%s\n", status, string(body))
			if status >= 300 {
				panic(string(body))
			}
			var created map[string]any
			_ = json.Unmarshal(body, &created)
			traderID = fmt.Sprint(created["trader_id"])
		} else {
			body, status, err := jsonReq(http.MethodPut, "/api/traders/"+traderID, payload)
			if err != nil {
				panic(err)
			}
			fmt.Printf("update trader status=%d\n", status)
			if status >= 300 {
				panic(string(body))
			}
		}

		stopBody, _, err := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/stop", nil)
		if err != nil {
			fmt.Printf("stop warn: %v\n", err)
		} else {
			fmt.Printf("stop: %s\n", string(stopBody))
		}
		time.Sleep(3 * time.Second)
		startBody, startStatus, err := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/start", nil)
		if err != nil {
			fmt.Printf("start warn: %v\n", err)
			continue
		}
		fmt.Printf("start status=%d body=%s\n", startStatus, string(startBody))
		if startStatus >= 300 {
			fmt.Printf("start failed for %s\n", spec.TraderName)
		}
	}
	fmt.Println("\nDone — 4 copy bots on new leaders (copy_on_start=false, $50, fills mode)")
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok && fmt.Sprint(v) != "" {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}

func shortAddr(a string) string {
	if len(a) <= 12 {
		return a
	}
	return a[:6] + "..." + a[len(a)-4:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
