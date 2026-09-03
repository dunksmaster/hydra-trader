//go:build ignore

// Disable all copy overflow to Crypto BigG; keep BigG + machibigbrother stopped.
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

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "disable-overflow@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}

	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b
	}
	put := func(path string, body any) (int, string) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, strings.TrimSpace(string(b))
	}

	biggID := ""
	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name", "name"))
		id := firstStr(tr, "trader_id", "id")
		if strings.Contains(name, "bigg") {
			biggID = id
		}
		if fmt.Sprint(tr["is_running"]) == "true" && (strings.Contains(name, "bigg") || strings.Contains(name, "machibigbrother")) {
			code, body := post("/api/traders/" + id + "/stop")
			fmt.Printf("stop %s status=%d %s\n", firstStr(tr, "trader_name", "name"), code, truncate(body, 80))
		}
	}

	disabled := 0
	for _, tr := range traders {
		sid := firstStr(tr, "strategy_id")
		if sid == "" {
			continue
		}
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil {
			continue
		}
		overflowOn := fmt.Sprint(cc["overflow_enabled"]) == "true"
		overflowID := strings.TrimSpace(fmt.Sprint(cc["overflow_trader_id"]))
		if !overflowOn && overflowID == "" {
			continue
		}
		if biggID != "" && overflowID != "" && overflowID != biggID {
			continue
		}
		cc["overflow_enabled"] = false
		cc["overflow_trader_id"] = ""
		cfg["copy_config"] = cc
		code, body := put("/api/strategies/"+sid, map[string]any{"config": cfg})
		fmt.Printf("disable overflow %s status=%d %s\n", firstStr(tr, "trader_name", "name"), code, truncate(body, 60))
		if code == 200 {
			disabled++
		}
	}
	fmt.Printf("\nDisabled overflow on %d strategies. BigG=%s\n", disabled, biggID)
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
