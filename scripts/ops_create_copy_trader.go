//go:build ignore

// Create HL Copy strategy and Autopilot Copy trader (dry-run by default).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"nofx/auth"
)

const (
	userID          = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	autopilotTrader = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502"
	leaderAddress   = "0x6859da14835424957a1e6b397d8026b1d9ff7e1e"
	strategyName    = "HL Copy 0x6859"
	traderName      = "Autopilot Copy"
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
	token, err := auth.GenerateJWT(userID, "copy-trader@local")
	if err != nil {
		fmt.Fprintf(os.Stderr, "jwt: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{}
	authGet := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return body, resp.StatusCode
	}
	authJSON := func(method, path string, payload any) ([]byte, int) {
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
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return out, resp.StatusCode
	}

	// Load Autopilot trader for model + exchange reuse.
	apBody, apStatus := authGet("/api/traders/" + autopilotTrader + "/config")
	if apStatus >= 300 {
		fmt.Fprintf(os.Stderr, "get autopilot config failed status=%d body=%s\n", apStatus, string(apBody))
		os.Exit(1)
	}
	var apCfg map[string]any
	_ = json.Unmarshal(apBody, &apCfg)
	aiModelID := firstString(apCfg, "ai_model_id", "ai_model")
	exchangeID := fmt.Sprint(apCfg["exchange_id"])
	if aiModelID == "" || exchangeID == "" {
		fmt.Fprintf(os.Stderr, "autopilot missing ai_model or exchange_id: %v\n", apCfg)
		os.Exit(1)
	}
	fmt.Printf("Autopilot model=%s exchange=%s\n", aiModelID, exchangeID)

	// Find or create copy strategy.
	listBody, _ := authGet("/api/strategies")
	var listOut struct {
		Strategies []map[string]any `json:"strategies"`
	}
	_ = json.Unmarshal(listBody, &listOut)
	strategyID := ""
	for _, st := range listOut.Strategies {
		if fmt.Sprint(st["name"]) == strategyName {
			strategyID = fmt.Sprint(st["id"])
			fmt.Printf("Strategy exists: %s (%s)\n", strategyName, strategyID)
			break
		}
	}
	if strategyID == "" {
		createBody, createStatus := authJSON(http.MethodPost, "/api/strategies", map[string]any{
			"name":        strategyName,
			"description": "Hyperliquid copy mirror for wallet 0x6859...",
			"config": map[string]any{
				"strategy_type": "copy_trading",
				"language":      "en",
				"copy_config": map[string]any{
					"leader_address":   leaderAddress,
					"size_mode":        "fixed_notional",
					"notional_usd":     15,
					"min_notional_usd": 12,
					"max_notional_pct": 40,
					"max_leverage":     5,
					"exit_mode":        "leader_plus_stop",
					"safety_stop_pct":  15,
					"symbol_blocklist": []string{"xyz:"},
					"dry_run":          true,
					"inverse":          false,
				},
			},
		})
		if createStatus >= 300 {
			fmt.Fprintf(os.Stderr, "create strategy failed status=%d body=%s\n", createStatus, string(createBody))
			os.Exit(1)
		}
		var created map[string]any
		_ = json.Unmarshal(createBody, &created)
		strategyID = fmt.Sprint(created["id"])
		fmt.Printf("Created strategy %s (%s)\n", strategyName, strategyID)
	}

	// Find or create copy trader.
	tradersBody, _ := authGet("/api/traders")
	var tradersOut struct {
		Traders []map[string]any `json:"traders"`
	}
	_ = json.Unmarshal(tradersBody, &tradersOut)
	copyTraderID := ""
	for _, tr := range tradersOut.Traders {
		if fmt.Sprint(tr["name"]) == traderName || fmt.Sprint(tr["trader_name"]) == traderName {
			copyTraderID = fmt.Sprint(firstString(tr, "trader_id", "id"))
			fmt.Printf("Trader exists: %s (%s)\n", traderName, copyTraderID)
			break
		}
	}

	traderPayload := map[string]any{
		"name":                  traderName,
		"ai_model_id":           aiModelID,
		"exchange_id":           exchangeID,
		"strategy_id":           strategyID,
		"scan_interval_minutes": 5,
		"is_cross_margin":       true,
		"show_in_competition":   false,
	}
	if copyTraderID == "" {
		createTraderBody, createTraderStatus := authJSON(http.MethodPost, "/api/traders", traderPayload)
		if createTraderStatus >= 300 {
			fmt.Fprintf(os.Stderr, "create trader failed status=%d body=%s\n", createTraderStatus, string(createTraderBody))
			os.Exit(1)
		}
		var created map[string]any
		_ = json.Unmarshal(createTraderBody, &created)
		copyTraderID = fmt.Sprint(created["trader_id"])
		fmt.Printf("Created trader %s (%s)\n", traderName, copyTraderID)
	} else {
		updateBody, updateStatus := authJSON(http.MethodPut, "/api/traders/"+copyTraderID, traderPayload)
		if updateStatus >= 300 {
			fmt.Fprintf(os.Stderr, "update trader failed status=%d body=%s\n", updateStatus, string(updateBody))
			os.Exit(1)
		}
		fmt.Printf("Updated trader %s (%s)\n", traderName, copyTraderID)
	}

	fmt.Println("\nNext steps:")
	fmt.Println("1. Deploy the copy-trading build to Railway")
	fmt.Println("2. Stop NOFX Autopilot and close its open HYPE short")
	fmt.Println("3. POST /api/traders/" + copyTraderID + "/start")
	fmt.Println("4. Verify dry-run logs for 2-3 cycles, then set dry_run=false in Strategy Studio")
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok && fmt.Sprint(v) != "" {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}
