//go:build ignore

// Probe every API endpoint used by Telegram quick commands (owner JWT).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"nofx/auth"
)

const ownerID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"

func main() {
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/probe_tg_commands.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	token, err := auth.GenerateJWT(ownerID, "tg-cmd-probe@local")
	if err != nil {
		panic(err)
	}
	client := &http.Client{Timeout: 30 * time.Second}

	get := func(path string) (int, []byte) {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return 0, []byte(err.Error())
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, body
	}

	fmt.Println("=== Telegram command API probe ===")
	fmt.Printf("base=%s owner=%s\n\n", base, ownerID)

	checks := []struct {
		cmd  string
		path string
	}{
		{"/balanca", "/api/my-traders"},
		{"/positions", "/api/my-traders"},
		{"/pnl", "/api/my-traders"},
		{"/history", "/api/my-traders"},
		{"/copystatus", "/api/my-traders"},
		{"/telegram", "/api/telegram"},
	}

	for _, c := range checks {
		st, body := get(c.path)
		fmt.Printf("[%s] GET %s -> %d len=%d\n", c.cmd, c.path, st, len(body))
		if st >= 400 {
			fmt.Printf("  body: %s\n", trunc(string(body), 200))
			continue
		}
	}

	st, body := get("/api/my-traders")
	if st >= 400 {
		fmt.Printf("\nFAIL my-traders: %s\n", string(body))
		os.Exit(1)
	}

	var traders []map[string]any
	if err := json.Unmarshal(body, &traders); err != nil {
		var wrap struct {
			Traders []map[string]any `json:"traders"`
		}
		if json.Unmarshal(body, &wrap) == nil {
			traders = wrap.Traders
		}
	}
	fmt.Printf("\nTraders: %d\n", len(traders))
	running := 0
	withPos := 0
	for _, tr := range traders {
		name := fmt.Sprint(tr["trader_name"])
		id := fmt.Sprint(tr["trader_id"])
		ex := fmt.Sprint(tr["exchange"])
		run := fmt.Sprint(tr["is_running"]) == "true"
		if run {
			running++
		}

		acctSt, acctBody := get("/api/account?trader_id=" + id)
		posSt, posBody := get("/api/positions?trader_id=" + id)
		statSt, statBody := get("/api/statistics/full?trader_id=" + id)
		histSt, histBody := get("/api/positions/history?trader_id=" + id + "&limit=5")

		var acct map[string]any
		_ = json.Unmarshal(acctBody, &acct)
		var positions []map[string]any
		_ = json.Unmarshal(posBody, &positions)
		posN := len(positions)
		if posN > 0 {
			withPos++
		}

		fmt.Printf("\n--- %s (%s) ---\n", name, ex)
		fmt.Printf("  running=%v account=%d equity=%v pos=%d stats=%d history=%d\n",
			run, acctSt, acct["total_equity"], posSt, posN, statSt, histSt)
		if acctSt >= 400 {
			fmt.Printf("  account err: %s\n", trunc(string(acctBody), 120))
		}
		if posSt >= 400 {
			fmt.Printf("  positions err: %s\n", trunc(string(posBody), 120))
		}
		if statSt >= 400 {
			fmt.Printf("  stats err: %s\n", trunc(string(statBody), 120))
		}
		if histSt >= 400 {
			fmt.Printf("  history err: %s\n", trunc(string(histBody), 120))
		}
		if posN > 0 {
			p := positions[0]
			fmt.Printf("  sample: %s %s pnl=%v\n", p["symbol"], p["side"], p["unrealized_pnl"])
		}
	}

	fmt.Printf("\nSummary: %d traders, %d running, %d with open positions\n", len(traders), running, withPos)

	st, tgBody := get("/api/telegram")
	fmt.Printf("\n[/telegram config] status=%d\n%s\n", st, trunc(string(tgBody), 400))
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
