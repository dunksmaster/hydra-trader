//go:build ignore

// Stop all traders except Hyperdash e282 (single HL copy). lucy30 + BigG stay stopped.
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

func keep(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(low, "e282")
}

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "keep-fleet@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := client.Do(req)
		if err != nil || r == nil {
			return nil
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := client.Do(req)
		if err != nil || r == nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}
	put := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		r, _ := client.Do(req)
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := fmt.Sprint(tr["trader_name"])
		if keep(name) {
			fmt.Printf("KEEP %s running=%v\n", name, tr["is_running"])
			continue
		}
		id := fmt.Sprint(tr["trader_id"])
		if fmt.Sprint(tr["is_running"]) == "true" {
			code, body := post("/api/traders/" + id + "/stop")
			fmt.Printf("STOP %s → %d %s\n", name, code, trunc(body, 80))
		}
		sid := strings.TrimSpace(fmt.Sprint(tr["strategy_id"]))
		if sid == "" || sid == "<nil>" {
			continue
		}
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil {
			continue
		}
		if fmt.Sprint(cc["copy_paused"]) == "true" {
			continue
		}
		cc["copy_paused"] = true
		cfg["copy_config"] = cc
		code, _ := put("/api/strategies/"+sid, map[string]any{"config": cfg})
		fmt.Printf("  pause %s strategy %d\n", name, code)
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
