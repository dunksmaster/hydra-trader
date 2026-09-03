//go:build ignore

// BigG overflow copies machibigbrother only; enable live L1 copy on HL; Telegram → NVIDIA.
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

	"github.com/golang-jwt/jwt/v5"
)

const (
	userID       = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	leaderAddr   = "0x020ca66c30bec2c4fe3861a94e4db4a498a35872"
	machiName    = "machibigbrother"
	strategyName = "HL Copy machibigbrother"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_bigg_machibrother_only.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "bigg-machi@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}

	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0
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

	nvidiaModel := findNVIDIAModel(get)
	if nvidiaModel != "" {
		body, st, err := jsonReq(http.MethodPost, "/api/telegram/model", map[string]any{"model_id": nvidiaModel})
		if err != nil || st >= 300 {
			fmt.Printf("telegram model warn: status=%d err=%v body=%s\n", st, err, string(body))
		} else {
			fmt.Printf("Telegram AI model → NVIDIA (%s)\n", nvidiaModel)
		}
	} else {
		fmt.Println("warn: no NVIDIA model found — Telegram AI unchanged")
	}

	trBody, trSt := get("/api/my-traders")
	if trSt >= 300 || trSt == 0 {
		panic(fmt.Sprintf("my-traders status=%d", trSt))
	}
	var traders []map[string]any
	_ = json.Unmarshal(trBody, &traders)

	biggID := ""
	machiID := ""
	machiStrategyID := ""
	for _, tr := range traders {
		name := strings.ToLower(fmt.Sprint(tr["trader_name"]))
		id := firstStr(tr, "trader_id", "id")
		if strings.Contains(name, "bigg") && !strings.Contains(name, "autopilot") {
			biggID = id
		}
		if name == machiName || strings.Contains(name, "machibig") {
			machiID = id
			machiStrategyID = firstStr(tr, "strategy_id")
		}
	}
	if biggID == "" {
		panic("Crypto BigG trader not found")
	}
	if machiID == "" {
		panic("machibigbrother trader not found — run ops_add_watch_leader.go first")
	}
	fmt.Printf("BigG=%s machibigbrother=%s\n", biggID, machiID)

	// machibigbrother: live L1 copy + overflow → BigG only
	if machiStrategyID == "" {
		stBody, stSt := get("/api/strategies")
		if stSt > 0 && stSt < 300 {
			var arr []map[string]any
			if json.Unmarshal(stBody, &arr) == nil {
				for _, s := range arr {
					if fmt.Sprint(s["name"]) == strategyName {
						machiStrategyID = fmt.Sprint(s["id"])
						break
					}
				}
			}
		}
	}
	if machiStrategyID == "" || machiStrategyID == "<nil>" {
		panic("machibigbrother strategy not found")
	}
	if err := patchCopyStrategy(jsonReq, get, machiStrategyID, map[string]any{
		"leader_address":         leaderAddr,
		"copy_layer":             1,
		"copy_paused":            false,
		"dry_run":                false,
		"copy_on_start":          false,
		"overflow_enabled":       true,
		"overflow_trader_id":     biggID,
		"overflow_on_skip":       []string{"already_open", "max_positions", "margin"},
		"overflow_notional_usd":  50,
		"overflow_max_positions": 10,
		"max_positions":          5,
		"notional_usd":           50,
	}); err != nil {
		panic(err)
	}
	fmt.Println("machibigbrother → L1 live copy, overflow → BigG")

	// All strategies: disable overflow to BigG except machibigbrother
	disabled := 0
	stListBody, stListSt := get("/api/strategies")
	if stListSt > 0 && stListSt < 300 {
		var strategies []map[string]any
		if json.Unmarshal(stListBody, &strategies) == nil {
			for _, st := range strategies {
				sid := fmt.Sprint(st["id"])
				if sid == "" || sid == "<nil>" || sid == machiStrategyID {
					continue
				}
				cfg, _ := st["config"].(map[string]any)
				if cfg == nil {
					continue
				}
				copyCfg, _ := cfg["copy_config"].(map[string]any)
				if copyCfg == nil {
					continue
				}
				overflowID := strings.TrimSpace(fmt.Sprint(copyCfg["overflow_trader_id"]))
				overflowOn := fmt.Sprint(copyCfg["overflow_enabled"]) == "true"
				if !overflowOn && overflowID == "" {
					continue
				}
				if overflowID != "" && overflowID != biggID && !overflowOn {
					continue
				}
				name := fmt.Sprint(st["name"])
				if err := patchCopyStrategy(jsonReq, get, sid, map[string]any{
					"overflow_enabled":   false,
					"overflow_trader_id": "",
				}); err != nil {
					fmt.Printf("warn disable overflow %s: %v\n", name, err)
					continue
				}
				fmt.Printf("overflow OFF: %s\n", name)
				disabled++
			}
		}
	}
	fmt.Printf("disabled overflow on %d other copy strategies\n", disabled)

	startTrader(jsonReq, machiID, traders)
	startTrader(jsonReq, biggID, traders)

	fmt.Printf("\nDone — BigG mirrors machibigbrother leader %s only.\n", leaderAddr)
	fmt.Println("HL bot machibigbrother is L1 live; other bots no longer overflow to Bitget.")
}

func patchCopyStrategy(jsonReq func(string, string, any) ([]byte, int, error), get func(string) ([]byte, int), strategyID string, patch map[string]any) error {
	stRaw, stSt := get("/api/strategies/" + strategyID)
	if stSt >= 300 {
		return fmt.Errorf("GET strategy %s status=%d", strategyID, stSt)
	}
	var stMap map[string]any
	if err := json.Unmarshal(stRaw, &stMap); err != nil {
		return err
	}
	cfg, _ := stMap["config"].(map[string]any)
	if cfg == nil {
		return fmt.Errorf("strategy %s missing config", strategyID)
	}
	copyCfg, ok := cfg["copy_config"].(map[string]any)
	if !ok || copyCfg == nil {
		copyCfg = map[string]any{}
	}
	for k, v := range patch {
		copyCfg[k] = v
	}
	cfg["copy_config"] = copyCfg
	body, status, err := jsonReq(http.MethodPut, "/api/strategies/"+strategyID, map[string]any{"config": cfg})
	if err != nil || status >= 300 {
		return fmt.Errorf("PUT strategy %s status=%d err=%v body=%s", strategyID, status, err, string(body))
	}
	return nil
}

func startTrader(jsonReq func(string, string, any) ([]byte, int, error), traderID string, traders []map[string]any) {
	running := false
	for _, tr := range traders {
		if firstStr(tr, "trader_id", "id") == traderID {
			running = fmt.Sprint(tr["is_running"]) == "true"
			break
		}
	}
	if running {
		fmt.Printf("%s already running\n", traderID)
		return
	}
	body, status, err := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/start", nil)
	fmt.Printf("start %s status=%d err=%v body=%s\n", traderID, status, err, string(body))
}

func findNVIDIAModel(get func(string) ([]byte, int)) string {
	body, st := get("/api/models")
	if st >= 300 {
		return ""
	}
	var models []map[string]any
	if json.Unmarshal(body, &models) != nil {
		return ""
	}
	for _, m := range models {
		if fmt.Sprint(m["enabled"]) != "true" {
			continue
		}
		id := firstStr(m, "id")
		blob := strings.ToLower(fmt.Sprint(m["custom_model_name"], " ", m["custom_api_url"], " ", m["provider"]))
		if strings.Contains(blob, "nvidia") || strings.Contains(blob, "nemotron") {
			return id
		}
	}
	for _, m := range models {
		if fmt.Sprint(m["enabled"]) != "true" {
			continue
		}
		id := firstStr(m, "id")
		if strings.Contains(id, "claw402") {
			continue
		}
		if fmt.Sprint(m["provider"]) == "openai" {
			return id
		}
	}
	return ""
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
