//go:build ignore

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
	baseURL := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "stop-alpha-mp@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 180 * time.Second}

	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	putJSON := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, baseURL+path, bytes.NewReader(raw))
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

	targets := map[string]bool{"alpha 6859": true, "money printer": true}
	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)

	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		if !targets[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		sid := firstStr(tr, "strategy_id")
		running := fmt.Sprint(tr["is_running"]) == "true"
		fmt.Printf("=== %s running=%v ===\n", name, running)

		if sid != "" {
			var st map[string]any
			_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
			cfg, _ := st["config"].(map[string]any)
			if cfg == nil {
				cfg = map[string]any{}
			}
			cc, _ := cfg["copy_config"].(map[string]any)
			if cc != nil {
				cc["copy_paused"] = true
				cfg["copy_config"] = cc
				stCode, body := putJSON("/api/strategies/"+sid, map[string]any{"config": cfg})
				fmt.Printf("  copy_paused status=%d %s\n", stCode, truncate(body, 100))
			}
		}
		if running {
			code, body := post("/api/traders/" + id + "/stop")
			fmt.Printf("  stop status=%d %s\n", code, truncate(body, 100))
			time.Sleep(3 * time.Second)
		}
	}

	fmt.Println("\n--- fleet status ---")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		running := fmt.Sprint(tr["is_running"]) == "true"
		sid := firstStr(tr, "strategy_id")
		extra := ""
		if sid != "" {
			var st map[string]any
			_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
			if c, ok := st["config"].(map[string]any); ok {
				if cc, ok := c["copy_config"].(map[string]any); ok {
					extra = fmt.Sprintf(" paused=%v leader=%s", cc["copy_paused"], short(fmt.Sprint(cc["leader_address"])))
				} else {
					extra = " (AI / no copy)"
				}
			}
		}
		tag := "KEEP"
		if !running {
			tag = "OFF "
		}
		fmt.Printf("%s %-22s running=%v%s\n", tag, name, running, extra)
	}
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

func short(a string) string {
	if len(a) > 12 {
		return a[:8] + "…" + a[len(a)-4:]
	}
	return a
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
