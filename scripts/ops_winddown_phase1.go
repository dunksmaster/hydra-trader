//go:build ignore

// Phase 1 of project wind-down: stop all NEW opens without disturbing
// existing exit management.
//
//   - Every Hyperliquid copy-trading head gets copy_paused=true via its
//     strategy config. The trader process is left running (no /stop call)
//     so any code-enforced hard take-profit/stop-loss checks keep firing
//     on positions that are still open — copy_paused only blocks new opens,
//     per store/strategy.go's own doc comment on the field.
//   - Crypto BigG (Bitget, AI-decision — not a copy head, no pause lever
//     exists for it) gets a full /stop, since equity is already ~$0.25 and
//     there is no "block new opens only" mode available for it.
//
// This script makes NO other changes: nothing is closed, no positions are
// touched, nothing is deleted. It only prevents new opens going forward.
//
// Run with your own local NOFX_BASE_URL / JWT_SECRET already set:
//
//	go run scripts/ops_winddown_phase1.go
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

// The 10 copy-trading heads confirmed running as of 2026-09-03 (Railway logs,
// deployment 12a62b12, "Auto-starting trader" lines from the last restart).
// Matched by exact trader_name from /api/my-traders.
var copyHeadsToPause = map[string]bool{
	"lucy30":          true,
	"Hyperdash e282":  true,
	"Hyperdash b7e0":  true,
	"Hyperdash 364a":  true,
	"machibigbrother": true,
	"Alpha 6859":      true,
	"Copy L4":         true,
	"Money Printer":   true,
	"Grinder":         true,
	"🐉 Leviathan":     true,
}

// AI-decision trader with no "block new opens" lever — full stop only.
const aiTraderToStop = "Crypto BigG"

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — set it in your environment before running this")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "winddown-phase1@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to sign token:", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 90 * time.Second}

	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("  GET %s → err: %v\n", path, err)
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

	var traders []map[string]any
	if err := json.Unmarshal(get("/api/my-traders"), &traders); err != nil {
		fmt.Fprintln(os.Stderr, "could not parse /api/my-traders:", err)
		os.Exit(1)
	}
	fmt.Printf("traders=%d\n\n", len(traders))

	paused, missed, stoppedAI := []string{}, []string{}, false

	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		id := firstStr(tr, "trader_id", "id")
		sid := firstStr(tr, "strategy_id")

		switch {
		case copyHeadsToPause[strings.TrimSpace(name)]:
			if sid == "" || sid == "<nil>" {
				fmt.Printf("SKIP %s: no strategy_id, cannot set copy_paused\n", name)
				missed = append(missed, name)
				continue
			}
			var st map[string]any
			_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
			cfg, _ := st["config"].(map[string]any)
			if cfg == nil {
				cfg = map[string]any{}
			}
			cc, _ := cfg["copy_config"].(map[string]any)
			if cc == nil {
				fmt.Printf("SKIP %s: no copy_config on strategy %s (not actually a copy head?)\n", name, sid)
				missed = append(missed, name)
				continue
			}
			cc["copy_paused"] = true
			cfg["copy_config"] = cc
			status, body := putJSON("/api/strategies/"+sid, map[string]any{"config": cfg})
			fmt.Printf("PAUSE %-16s → status=%d %s\n", name, status, truncate(body, 100))
			if status < 300 {
				paused = append(paused, name)
			} else {
				missed = append(missed, name)
			}
			time.Sleep(1 * time.Second)

		case strings.EqualFold(name, aiTraderToStop):
			running := fmt.Sprint(tr["is_running"]) == "true"
			if !running {
				fmt.Printf("%s already stopped\n", aiTraderToStop)
				stoppedAI = true
				continue
			}
			status, body := post("/api/traders/" + id + "/stop")
			fmt.Printf("STOP  %-16s → status=%d %s\n", name, status, truncate(body, 100))
			stoppedAI = status < 300
		}
	}

	fmt.Println("\n========== RESULT ==========")
	fmt.Printf("copy_paused=true set on: %s\n", strings.Join(paused, ", "))
	if len(missed) > 0 {
		fmt.Printf("NOT paused (check manually): %s\n", strings.Join(missed, ", "))
	}
	fmt.Printf("%s stopped: %v\n", aiTraderToStop, stoppedAI)
	fmt.Println("\nNothing was closed and nothing was deleted. Existing open positions")
	fmt.Println("on the paused heads will still be managed/closed by their own exit logic.")
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
