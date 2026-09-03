//go:build ignore

// Enable HL → Bitget overflow on the five copy leaders and start Crypto BigG.
// Does not stop or PUT running copy bots (strategy reload happens on the next fill).
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

const userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"

var copyNames = []string{
	"leviathan",
	"grinder",
	"money printer",
	"copy l4",
	"alpha 6859",
}

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_set_copy_overflow.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "copy-overflow@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}

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

	trBody, st := get("/api/my-traders")
	if st >= 300 || st == 0 {
		panic(fmt.Sprintf("my-traders status=%d body=%s", st, string(trBody)))
	}
	var trList []map[string]any
	_ = json.Unmarshal(trBody, &trList)

	biggID := ""
	biggRunning := false
	copyTraders := []map[string]any{}
	for _, tr := range trList {
		name := strings.ToLower(fmt.Sprint(tr["trader_name"], " ", tr["name"]))
		if strings.Contains(name, "crypto bigg") || (strings.Contains(name, "bigg") && !strings.Contains(name, "autopilot")) {
			biggID = firstStr(tr, "trader_id", "id")
			biggRunning = fmt.Sprint(tr["is_running"]) == "true"
		}
		for _, needle := range copyNames {
			if strings.Contains(name, needle) {
				copyTraders = append(copyTraders, tr)
				break
			}
		}
	}
	if biggID == "" {
		panic("Crypto BigG trader not found")
	}
	fmt.Printf("overflow venue Crypto BigG id=%s running=%v\n", biggID, biggRunning)

	seen := map[string]bool{}
	updated := 0
	for _, tr := range copyTraders {
		sid := firstStr(tr, "strategy_id")
		name := firstStr(tr, "trader_name", "name")
		if sid == "" || seen[sid] {
			continue
		}
		seen[sid] = true
		stRaw, stStatus := get("/api/strategies/" + sid)
		if stStatus >= 300 {
			panic(fmt.Sprintf("GET strategy %s status=%d %s", sid, stStatus, string(stRaw)))
		}
		var stMap map[string]any
		_ = json.Unmarshal(stRaw, &stMap)
		cfg, _ := stMap["config"].(map[string]any)
		if cfg == nil {
			panic("strategy " + sid + " has no config")
		}
		copyCfg, _ := cfg["copy_config"].(map[string]any)
		if copyCfg == nil {
			copyCfg = map[string]any{}
		}
		copyCfg["max_positions"] = 10
		copyCfg["overflow_enabled"] = true
		copyCfg["overflow_trader_id"] = biggID
		copyCfg["overflow_on_skip"] = []string{"already_open", "max_positions", "margin"}
		copyCfg["overflow_notional_usd"] = 50
		copyCfg["overflow_max_positions"] = 10
		cfg["copy_config"] = copyCfg
		body, status, err := jsonReq(http.MethodPut, "/api/strategies/"+sid, map[string]any{"config": cfg})
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("PUT strategy %s status=%d err=%v body=%s", sid, status, err, string(body)))
		}
		fmt.Printf("overflow enabled on %s strategy=%s\n", name, sid)
		updated++
	}
	fmt.Printf("updated %d copy strategies\n", updated)

	if !biggRunning {
		body, status, err := jsonReq(http.MethodPost, "/api/traders/"+biggID+"/start", nil)
		if err != nil {
			fmt.Printf("BigG start warn: %v\n", err)
		} else {
			fmt.Printf("BigG start status=%d %s\n", status, string(body))
		}
	} else {
		fmt.Println("Crypto BigG already running — leaving AI on")
	}
	fmt.Println("Done — next HL already_open/max_positions skip opens on Bitget. Fund BigG first if equity is tiny.")
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
