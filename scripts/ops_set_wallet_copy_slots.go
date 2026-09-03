//go:build ignore

// Set wallet_copy_slots on all copy_trading strategies (shared HL wallet margin reservation).
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
	walletCopySlots = 3
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT(userID, "wallet-slots@local")
	client := &http.Client{Timeout: 90 * time.Second}

	getJSON := func(path string) ([]byte, int) {
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

	putStrategy := func(strategyID string, cfg map[string]any) {
		payload := map[string]any{"config": cfg}
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
		fmt.Printf("  strategy %s update status=%d\n", strategyID, resp.StatusCode)
		if resp.StatusCode >= 300 {
			panic(string(out))
		}
	}

	var trList []map[string]any
	raw, status := getJSON("/api/my-traders")
	if status >= 300 {
		panic(fmt.Sprintf("GET /api/my-traders status=%d body=%s", status, string(raw)))
	}
	if err := json.Unmarshal(raw, &trList); err != nil {
		var wrap map[string]any
		_ = json.Unmarshal(raw, &wrap)
		if items, ok := wrap["traders"].([]any); ok {
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					trList = append(trList, m)
				}
			}
		}
	}

	seenStrategy := map[string]bool{}
	updated := 0
	for _, tr := range trList {
		name := fmt.Sprint(tr["name"], tr["trader_name"])
		if !strings.Contains(strings.ToLower(name), "copy") {
			continue
		}
		sid := fmt.Sprint(tr["strategy_id"])
		if sid == "" || sid == "<nil>" || seenStrategy[sid] {
			continue
		}
		seenStrategy[sid] = true

		stRaw, stStatus := getJSON("/api/strategies/" + sid)
		if stStatus >= 300 {
			fmt.Printf("skip strategy %s: status=%d\n", sid, stStatus)
			continue
		}
		var st map[string]any
		if err := json.Unmarshal(stRaw, &st); err != nil {
			panic(err)
		}
		cfg, _ := st["config"].(map[string]any)
		if cfg == nil || fmt.Sprint(cfg["strategy_type"]) != "copy_trading" {
			continue
		}
		copyCfg, _ := cfg["copy_config"].(map[string]any)
		if copyCfg == nil {
			copyCfg = map[string]any{}
		}
		copyCfg["wallet_copy_slots"] = walletCopySlots
		cfg["copy_config"] = copyCfg
		fmt.Printf("Updating strategy for %s wallet_copy_slots=%d\n", strings.TrimSpace(name), walletCopySlots)
		putStrategy(sid, cfg)
		updated++
	}
	fmt.Printf("Updated %d copy strategies\n", updated)

	for _, tr := range trList {
		name := fmt.Sprint(tr["name"], tr["trader_name"])
		if !strings.Contains(strings.ToLower(name), "copy") {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		if id == "" {
			continue
		}
		fmt.Printf("Restarting %s (%s)\n", strings.TrimSpace(name), id[:min(28, len(id))])
		req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/traders/"+id+"/stop", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		client.Do(req)
		time.Sleep(1500 * time.Millisecond)
		req2, _ := http.NewRequest(http.MethodPost, baseURL+"/api/traders/"+id+"/start", nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req2)
		if err != nil {
			panic(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("  start status=%d body=%s\n", resp.StatusCode, string(b))
	}
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "" {
			return fmt.Sprint(v)
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
