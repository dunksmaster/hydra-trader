//go:build ignore

// Clone a strategy for BigG: prefer a public market strategy with visible
// config; otherwise duplicate the Autopilot strategy as a named clone.
// Then assign it to Crypto BigG and optionally start the bot.
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
	userID   = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	traderID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	autopilotStrategyID = "2e50a1e7-cb16-4d0f-8ed7-c8ea6cda3ad3"
	cloneName           = "Market Test - BigG Mar 2026"
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
	token, err := auth.GenerateJWT(userID, "market-clone@local")
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

	// Step 1: try public strategy market (clone via create from visible config)
	sourceID := ""
	sourceName := ""
	publicBody, _ := authGet("/api/strategies/public")
	var publicOut struct {
		Strategies []map[string]any `json:"strategies"`
	}
	_ = json.Unmarshal(publicBody, &publicOut)
	for _, st := range publicOut.Strategies {
		if st["config_visible"] == true && st["config"] != nil {
			sourceID = fmt.Sprint(st["id"])
			sourceName = fmt.Sprint(st["name"])
			break
		}
	}
	if sourceID != "" {
		fmt.Printf("Using public market strategy: %s (%s)\n", sourceName, sourceID)
	} else {
		fmt.Println("No public strategies with visible config; duplicating Autopilot strategy")
		sourceID = autopilotStrategyID
		sourceName = "NOFX Hyperliquid Crypto Luna"
	}

	// Step 2: check if clone already exists
	listBody, _ := authGet("/api/strategies")
	var listOut struct {
		Strategies []map[string]any `json:"strategies"`
	}
	_ = json.Unmarshal(listBody, &listOut)
	var cloneID string
	for _, st := range listOut.Strategies {
		if fmt.Sprint(st["name"]) == cloneName {
			cloneID = fmt.Sprint(st["id"])
			fmt.Printf("Clone already exists: %s (%s)\n", cloneName, cloneID)
			break
		}
	}

	// Step 3: create clone if missing
	if cloneID == "" {
		if sourceID == autopilotStrategyID || len(publicOut.Strategies) == 0 {
			dupBody, dupStatus := authJSON(http.MethodPost, "/api/strategies/"+sourceID+"/duplicate", map[string]string{
				"name": cloneName,
			})
			if dupStatus >= 300 {
				fmt.Fprintf(os.Stderr, "duplicate failed status=%d body=%s\n", dupStatus, string(dupBody))
				os.Exit(1)
			}
			var dupOut map[string]any
			_ = json.Unmarshal(dupBody, &dupOut)
			cloneID = fmt.Sprint(dupOut["id"])
			fmt.Printf("Duplicated %s -> %s (%s)\n", sourceName, cloneName, cloneID)
		} else {
			// Public strategy with config: create owned copy
			var src map[string]any
			for _, st := range publicOut.Strategies {
				if fmt.Sprint(st["id"]) == sourceID {
					src = st
					break
				}
			}
			createBody, createStatus := authJSON(http.MethodPost, "/api/strategies", map[string]any{
				"name":        cloneName,
				"description": "Cloned from Strategy Market: " + sourceName,
				"config":      src["config"],
			})
			if createStatus >= 300 {
				fmt.Fprintf(os.Stderr, "create failed status=%d body=%s\n", createStatus, string(createBody))
				os.Exit(1)
			}
			var created map[string]any
			_ = json.Unmarshal(createBody, &created)
			cloneID = fmt.Sprint(created["id"])
			fmt.Printf("Created from market %s -> %s (%s)\n", sourceName, cloneName, cloneID)
		}
	}

	// Step 4: review clone config summary
	stratBody, stratStatus := authGet("/api/strategies/" + cloneID)
	if stratStatus >= 300 {
		fmt.Fprintf(os.Stderr, "get strategy failed: %s\n", string(stratBody))
		os.Exit(1)
	}
	var strat map[string]any
	_ = json.Unmarshal(stratBody, &strat)
	cfg, _ := strat["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	if ai == nil {
		ai = cfg
	}
	fmt.Printf("\n=== Clone review ===\n")
	fmt.Printf("name=%v\n", strat["name"])
	fmt.Printf("coin_source=%s\n", mustJSON(ai["coin_source"]))
	fmt.Printf("risk_control=%s\n", mustJSON(ai["risk_control"]))

	// Step 5: assign to BigG
	traderBody, traderStatus := authGet("/api/traders/" + traderID + "/config")
	if traderStatus >= 300 {
		fmt.Fprintf(os.Stderr, "get trader config failed: %s\n", string(traderBody))
		os.Exit(1)
	}
	var trader map[string]any
	_ = json.Unmarshal(traderBody, &trader)
	aiModelID := trader["ai_model_id"]
	if aiModelID == nil || fmt.Sprint(aiModelID) == "" {
		aiModelID = trader["ai_model"]
	}
	isCross := false
	if v, ok := trader["is_cross_margin"].(bool); ok {
		isCross = v
	}
	showComp := true
	if v, ok := trader["show_in_competition"].(bool); ok {
		showComp = v
	}
	scanMin := 10
	if v, ok := trader["scan_interval_minutes"].(float64); ok && v > 0 {
		scanMin = int(v)
	}
	updatePayload := map[string]any{
		"name":                  trader["trader_name"],
		"ai_model_id":           aiModelID,
		"exchange_id":           trader["exchange_id"],
		"strategy_id":           cloneID,
		"is_cross_margin":       isCross,
		"show_in_competition":   showComp,
		"scan_interval_minutes": scanMin,
	}
	upBody, upStatus := authJSON(http.MethodPut, "/api/traders/"+traderID, updatePayload)
	if upStatus >= 300 {
		fmt.Fprintf(os.Stderr, "update trader failed status=%d body=%s\n", upStatus, string(upBody))
		os.Exit(1)
	}
	fmt.Printf("\nAssigned clone %s to Crypto BigG\n", cloneID)

	// Step 6: verify Autopilot unchanged
	apID := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502"
	apBody, _ := authGet("/api/traders/" + apID + "/config")
	var ap map[string]any
	_ = json.Unmarshal(apBody, &ap)
	fmt.Printf("Autopilot strategy_id=%v strategy_name=%v (unchanged expected)\n", ap["strategy_id"], ap["strategy_name"])

	// Step 7: start BigG unless NO_START=1
	if os.Getenv("NO_START") == "1" {
		fmt.Println("NO_START=1, skipping start")
		return
	}
	startBody, startStatus := authJSON(http.MethodPost, "/api/traders/"+traderID+"/start", nil)
	fmt.Printf("\nStart BigG status=%d body=%s\n", startStatus, string(startBody))
	if startStatus >= 300 {
		os.Exit(1)
	}
}

func mustJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
