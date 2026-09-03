//go:build ignore

// Stop the 7 bots from NOFX-STOP-COPY-BOTS-NOW.md; leave only Hyperdash e282 running.
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

func stopName(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(low, "hyperdash e282"):
		return false // only keeper
	case strings.Contains(low, "hyperdash b7e0"):
		return true
	case strings.Contains(low, "hyperdash 364a"):
		return true
	case strings.Contains(low, "machibigbrother"):
		return true
	case strings.Contains(low, "copy l4"):
		return true
	case strings.Contains(low, "leviathan"):
		return true
	case strings.Contains(low, "grinder"):
		return true
	case strings.Contains(low, "alpha 6859"):
		return true
	case strings.Contains(low, "money printer"):
		return true
	default:
		return false
	}
}

func main() {
	baseURL := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "stop-seven@local",
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

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)

	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		if !stopName(name) {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		sid := firstStr(tr, "strategy_id")
		running := fmt.Sprint(tr["is_running"]) == "true"
		fmt.Printf("STOP %s running=%v\n", name, running)

		if sid != "" && sid != "<nil>" {
			var st map[string]any
			_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
			cfg, _ := st["config"].(map[string]any)
			if cfg != nil {
				if cc, ok := cfg["copy_config"].(map[string]any); ok && cc != nil {
					cc["copy_paused"] = true
					cfg["copy_config"] = cc
					code, body := putJSON("/api/strategies/"+sid, map[string]any{"config": cfg})
					fmt.Printf("  copy_paused status=%d %s\n", code, truncate(body, 80))
				}
			}
		}
		if running {
			for attempt := 1; attempt <= 3; attempt++ {
				code, body := post("/api/traders/" + id + "/stop")
				fmt.Printf("  stop attempt %d status=%d %s\n", attempt, code, truncate(body, 100))
				if code == 200 {
					break
				}
				time.Sleep(5 * time.Second)
			}
		}
		time.Sleep(2 * time.Second)
	}

	fmt.Println("\n=== FLEET STATUS ===")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		running := fmt.Sprint(tr["is_running"]) == "true"
		sid := firstStr(tr, "strategy_id")
		paused := ""
		if sid != "" {
			var st map[string]any
			_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
			if c, ok := st["config"].(map[string]any); ok {
				if cc, ok := c["copy_config"].(map[string]any); ok {
					paused = fmt.Sprintf(" paused=%v", cc["copy_paused"])
				}
			}
		}
		tag := "OFF"
		if running {
			tag = "ON "
		}
		fmt.Printf("%s %-22s %s%s\n", tag, name, map[bool]string{true: "RUNNING", false: "stopped"}[running], paused)
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
