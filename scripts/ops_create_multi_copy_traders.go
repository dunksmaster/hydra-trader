//go:build ignore

// Create Copy Leader 2 and 3 bots (fills mode, max_positions=1, $50 each).
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
	userID          = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	templateTrader  = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468"
	leader2         = "0xb8eb97eaed8367079894d2f1bed69bd220ec1dd5" // ETH-focused, ~4.5 trades/day
	leader3         = "0xf29c6bc1147a841519b382459a6d7a373c6b9971" // HYPE-focused, distinct from BTC/ETH leaders
)

type botSpec struct {
	StrategyName string
	TraderName   string
	Leader       string
}

var bots = []botSpec{
	{StrategyName: "HL Copy L2", TraderName: "Copy Leader 2", Leader: leader2},
	{StrategyName: "HL Copy L3", TraderName: "Copy Leader 3", Leader: leader3},
}

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT(userID, "multi-copy@local")
	client := &http.Client{Timeout: 90 * time.Second}

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
	jsonReq := func(method, path string, payload any) ([]byte, int) {
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, baseURL+path, body)
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		out, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return out, resp.StatusCode
	}

	tBody, tStatus := get("/api/traders/" + templateTrader + "/config")
	if tStatus >= 300 {
		panic(fmt.Sprintf("template trader status=%d body=%s", tStatus, string(tBody)))
	}
	var tpl map[string]any
	_ = json.Unmarshal(tBody, &tpl)
	aiModelID := firstString(tpl, "ai_model_id", "ai_model")
	exchangeID := fmt.Sprint(tpl["exchange_id"])
	fmt.Printf("template model=%s exchange=%s\n", aiModelID, exchangeID)

	stBody, _ := get("/api/strategies")
	var stList struct {
		Strategies []map[string]any `json:"strategies"`
	}
	_ = json.Unmarshal(stBody, &stList)
	strategyByName := map[string]string{}
	for _, st := range stList.Strategies {
		strategyByName[fmt.Sprint(st["name"])] = fmt.Sprint(st["id"])
	}

	trBody, _ := get("/api/traders")
	var trList struct {
		Traders []map[string]any `json:"traders"`
	}
	_ = json.Unmarshal(trBody, &trList)
	traderByName := map[string]string{}
	for _, tr := range trList.Traders {
		name := fmt.Sprint(tr["name"], tr["trader_name"])
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
			"exit_mode":              "leader_plus_stop",
			"safety_stop_pct":        15,
			"symbol_blocklist":       []string{},
			"reconcile_interval_sec": 60,
			"copy_on_start":          true,
			"min_leader_fill_usd":    10,
			"dry_run":                false,
			"inverse":                false,
		}
	}

	for _, spec := range bots {
		fmt.Printf("\n=== %s ===\n", spec.TraderName)
		strategyID := strategyByName[spec.StrategyName]
		if strategyID == "" || strategyID == "<nil>" {
			body, status := jsonReq(http.MethodPost, "/api/strategies", map[string]any{
				"name":        spec.StrategyName,
				"description": "Multi-leader copy slot for " + shortAddr(spec.Leader),
				"config": map[string]any{
					"strategy_type": "copy_trading",
					"language":      "en",
					"copy_config":   copyConfig(spec.Leader),
				},
			})
			fmt.Printf("create strategy status=%d body=%s\n", status, string(body))
			if status >= 300 {
				os.Exit(1)
			}
			var created map[string]any
			_ = json.Unmarshal(body, &created)
			strategyID = fmt.Sprint(created["id"])
		} else {
			body, status := jsonReq(http.MethodPut, "/api/strategies/"+strategyID, map[string]any{
				"config": map[string]any{
					"strategy_type": "copy_trading",
					"language":      "en",
					"copy_config":   copyConfig(spec.Leader),
				},
			})
			fmt.Printf("update strategy status=%d body=%s\n", status, string(body))
			if status >= 300 {
				os.Exit(1)
			}
		}
		fmt.Printf("strategy_id=%s leader=%s\n", strategyID, spec.Leader)

		traderID := traderByName[spec.TraderName]
		traderPayload := map[string]any{
			"name":                  spec.TraderName,
			"ai_model_id":           aiModelID,
			"exchange_id":           exchangeID,
			"strategy_id":           strategyID,
			"scan_interval_minutes": 5,
			"is_cross_margin":       true,
			"show_in_competition":   false,
		}
		if traderID == "" {
			body, status := jsonReq(http.MethodPost, "/api/traders", traderPayload)
			fmt.Printf("create trader status=%d body=%s\n", status, string(body))
			if status >= 300 {
				os.Exit(1)
			}
			var created map[string]any
			_ = json.Unmarshal(body, &created)
			traderID = fmt.Sprint(created["trader_id"])
		} else {
			body, status := jsonReq(http.MethodPut, "/api/traders/"+traderID, traderPayload)
			fmt.Printf("update trader status=%d body=%s\n", status, string(body))
			if status >= 300 {
				os.Exit(1)
			}
		}
		fmt.Printf("trader_id=%s\n", traderID)

		stopBody, stopStatus := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/stop", nil)
		fmt.Printf("stop status=%d body=%s\n", stopStatus, string(stopBody))
		time.Sleep(2 * time.Second)
		startBody, startStatus := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/start", nil)
		fmt.Printf("start status=%d body=%s\n", startStatus, string(startBody))
		if startStatus >= 300 {
			os.Exit(1)
		}
	}
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
