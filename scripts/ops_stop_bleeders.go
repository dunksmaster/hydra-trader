//go:build ignore

// Stop bleeders + underperformers. Leaves keepers running (copy-only fleet).
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

// Exact trader_name matches from /api/my-traders (Leviathan may include emoji).
var stopExact = map[string]bool{
	"Crypto BigG":     true,
	"NOFX Autopilot":  true,
	"Copy L4":         true,
	"Grinder":         true,
	"Money Printer":   false, // leave unless user asked — not in this stop list
	"Alpha 6859":      false,
}

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "stop-bleed@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}

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

	shouldStop := func(name string) bool {
		n := strings.TrimSpace(name)
		if stopExact[n] {
			return true
		}
		low := strings.ToLower(n)
		if strings.Contains(low, "leviathan") {
			return true
		}
		if strings.Contains(low, "crypto bigg") || strings.Contains(low, "bigg") && strings.Contains(low, "crypto") {
			return true
		}
		if strings.EqualFold(n, "Copy L4") || strings.EqualFold(n, "Grinder") {
			return true
		}
		if strings.EqualFold(n, "NOFX Autopilot") {
			return true
		}
		return false
	}

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	fmt.Printf("traders=%d\n", len(traders))

	stopped := []string{}
	paused := []string{}
	leftRunning := []string{}

	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		id := firstStr(tr, "trader_id", "id")
		sid := firstStr(tr, "strategy_id")
		running := fmt.Sprint(tr["is_running"]) == "true"
		if !shouldStop(name) {
			if running {
				leftRunning = append(leftRunning, name)
			}
			continue
		}
		fmt.Printf("\n=== STOP %s (running=%v) ===\n", name, running)

		// Pause copy_config so a restart cannot resume copying this leader.
		if sid != "" && sid != "<nil>" {
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
				status, body := putJSON("/api/strategies/"+sid, map[string]any{"config": cfg})
				fmt.Printf("  copy_paused=true status=%d %s\n", status, truncate(body, 120))
				paused = append(paused, name)
			} else {
				fmt.Printf("  (no copy_config — AI trader, stop only)\n")
			}
		}

		if running {
			status, body := post("/api/traders/" + id + "/stop")
			fmt.Printf("  stop status=%d %s\n", status, truncate(body, 120))
			stopped = append(stopped, name)
			time.Sleep(2 * time.Second)
		} else {
			fmt.Printf("  already stopped\n")
			stopped = append(stopped, name+" (was already stopped)")
		}
	}

	fmt.Println("\n========== RESULT ==========")
	fmt.Printf("Stopped: %s\n", strings.Join(stopped, ", "))
	fmt.Printf("Copy-paused: %s\n", strings.Join(paused, ", "))
	fmt.Printf("Still running (keepers): %s\n", strings.Join(leftRunning, ", "))
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if m == nil {
			continue
		}
		v, ok := m[k]
		if !ok {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" && s != "<nil>" {
			return s
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
