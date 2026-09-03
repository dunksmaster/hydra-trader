//go:build ignore

// Ensure single copy bot (Leviathan) is running live with fills mode.
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
	userID       = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	leviathanID  = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787218997"
	leader       = "0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf"
	strategyName = "HL Copy Leviathan"
	traderName   = "🐉 Leviathan"
)

func main() {
	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_ensure_copy_running.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT(userID, "ensure-copy@local")
	client := &http.Client{Timeout: 120 * time.Second}

	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b, resp.StatusCode
	}
	post := func(path string, body any) ([]byte, int) {
		var r io.Reader
		if body != nil {
			j, _ := json.Marshal(body)
			r = bytes.NewReader(j)
		}
		req, _ := http.NewRequest(http.MethodPost, base+path, r)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b, resp.StatusCode
	}

	// Resolve Leviathan trader id if it changed
	trBody, _ := get("/api/my-traders")
	var trs []map[string]any
	_ = json.Unmarshal(trBody, &trs)
	traderID := leviathanID
	var strategyID string
	for _, tr := range trs {
		name := fmt.Sprint(tr["trader_name"])
		if name == traderName || strings.Contains(name, "Leviathan") {
			traderID = fmt.Sprint(tr["trader_id"])
			strategyID = fmt.Sprint(tr["strategy_id"])
			break
		}
	}

	copyCfg := map[string]any{
		"leader_address":         leader,
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

	if strategyID != "" && strategyID != "<nil>" {
		req, _ := http.NewRequest(http.MethodPut, base+"/api/strategies/"+strategyID, bytes.NewReader(mustJSON(map[string]any{
			"config": map[string]any{
				"strategy_type": "copy_trading",
				"language":      "en",
				"copy_config":   copyCfg,
			},
		})))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			out, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("strategy update status=%d %s\n", resp.StatusCode, string(out))
		}
	}

	// Stop NOFX Autopilot — same HL wallet; must not compete with copy bot
	for _, tr := range trs {
		name := fmt.Sprint(tr["trader_name"])
		id := fmt.Sprint(tr["trader_id"])
		if strings.Contains(name, "NOFX Autopilot") {
			b, st := post("/api/traders/"+id+"/stop", nil)
			fmt.Printf("Autopilot stop status=%d %s\n", st, string(b))
		}
	}

	// Restart Leviathan for clean WS + config
	b, st := post("/api/traders/"+traderID+"/stop", nil)
	fmt.Printf("Leviathan stop status=%d %s\n", st, string(b))
	time.Sleep(3 * time.Second)
	b, st = post("/api/traders/"+traderID+"/start", nil)
	fmt.Printf("Leviathan start status=%d %s\n", st, string(b))
	if st >= 300 {
		os.Exit(1)
	}

	time.Sleep(2 * time.Second)
	acct, _ := get("/api/account?trader_id=" + traderID)
	fmt.Printf("\naccount: %s\n", acct)
	fmt.Println("\nDone — Leviathan auto copy live (new leader opens only).")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
