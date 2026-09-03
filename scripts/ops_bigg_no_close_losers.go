//go:build ignore

// Crypto BigG: block AI from closing underwater positions; losers exit via SL/TP only.
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
	biggTraderID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
)

const loserExitRule = `# Loser exits — SL/TP only (code-enforced)

If a position has NEGATIVE unrealized margin PnL:
- NEVER output close_long or close_short for that symbol.
- Do not close losers for margin relief, rotation, max_positions, age, or "cleanup".
- Losing positions exit ONLY when code-enforced stop-loss (hard_stop_loss_margin_pct) or take-profit fires, or the exchange protective bracket hits.
- You MAY still close profitable positions (positive margin PnL) when your normal exit rules apply.
- Output hold for underwater positions every cycle until the code stop or bracket closes them.`

func main() {
	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	if len(auth.JWTSecret) == 0 {
		fatal("JWT_SECRET missing")
	}
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "bigg-no-close-losers@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	c := &http.Client{Timeout: 120 * time.Second}
	get := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := c.Do(req)
		if err != nil {
			fatal("GET %s: %v", path, err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}
	put := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		r, err := c.Do(req)
		if err != nil {
			fatal("PUT %s: %v", path, err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}

	var traders []map[string]any
	_ = json.Unmarshal(mustGet(c, token, base+"/api/my-traders"), &traders)
	var sid string
	for _, tr := range traders {
		if fmt.Sprint(tr["trader_id"]) == biggTraderID {
			sid = strings.TrimSpace(fmt.Sprint(tr["strategy_id"]))
			break
		}
	}
	if sid == "" || sid == "<nil>" {
		fatal("Crypto BigG strategy_id not found")
	}

	st := get("/api/strategies/" + sid)
	cfg, _ := st["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	if ai == nil {
		fatal("missing ai_config")
	}
	rc, _ := ai["risk_control"].(map[string]any)
	if rc == nil {
		rc = map[string]any{}
	}
	rc["block_ai_close_on_loss"] = true
	ai["risk_control"] = rc

	sections, _ := ai["prompt_sections"].(map[string]any)
	if sections == nil {
		sections = map[string]any{}
	}
	tf := strings.TrimSpace(fmt.Sprint(sections["trading_frequency"]))
	if !strings.Contains(tf, "SL/TP only") {
		if tf != "" && tf != "<nil>" {
			tf += "\n\n"
		}
		tf += loserExitRule
	}
	sections["trading_frequency"] = tf

	dp := strings.TrimSpace(fmt.Sprint(sections["decision_process"]))
	if !strings.Contains(dp, "NEVER output close") {
		if dp != "" && dp != "<nil>" {
			dp += "\n\n"
		}
		dp += "5. Underwater positions (negative margin PnL): output hold only — NEVER close_long/close_short. Wait for code-enforced SL or TP."
	}
	sections["decision_process"] = dp
	ai["prompt_sections"] = sections
	cfg["ai_config"] = ai

	code, body := put("/api/strategies/"+sid, map[string]any{
		"name":        st["name"],
		"description": st["description"],
		"config":      cfg,
	})
	fmt.Printf("strategy patch %d %s\n", code, trunc(body, 120))
	if code >= 300 {
		os.Exit(1)
	}

	st2 := get("/api/strategies/" + sid)
	cfg2, _ := st2["config"].(map[string]any)
	ai2, _ := cfg2["ai_config"].(map[string]any)
	rc2, _ := ai2["risk_control"].(map[string]any)
	fmt.Printf("verify block_ai_close_on_loss=%v\n", rc2["block_ai_close_on_loss"])
	fmt.Println("✅ BigG will not AI-close underwater positions (code + prompt rule)")
}

func mustGet(c *http.Client, token, url string) []byte {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := c.Do(req)
	if err != nil {
		fatal("%v", err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	return b
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
