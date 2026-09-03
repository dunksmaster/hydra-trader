//go:build ignore

// Copy-trading PnL report: per-trader closed-position stats + leader mapping
// from copy_config, across all exchanges (Hyperliquid + Bitget).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"nofx/auth"

	"github.com/golang-jwt/jwt/v5"
)

const userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"

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
		UserID: userID, Email: "pnl-report@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 180 * time.Second}

	getRaw := func(path string) []byte {
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

	var traders []map[string]any
	if b := getRaw("/api/my-traders"); b != nil {
		_ = json.Unmarshal(b, &traders)
	}
	fmt.Fprintf(os.Stderr, "traders=%d\n", len(traders))

	type row struct {
		name, exchange, leader, layer string
		running                       bool
		stats                         map[string]any
		equity                        float64
		openPnL                       float64
		openPositions                 int
	}
	rows := []row{}

	for _, tr := range traders {
		id := firstStr(tr, "trader_id", "id")
		name := firstStr(tr, "trader_name", "name")
		sid := firstStr(tr, "strategy_id")
		if id == "" || name == "" || sid == "<nil>" || sid == "" {
			continue
		}
		r := row{name: name, running: fmt.Sprint(tr["is_running"]) == "true"}

		cfgBody := getRaw("/api/traders/" + id + "/config")
		var cfg map[string]any
		_ = json.Unmarshal(cfgBody, &cfg)
		r.exchange = firstStr(cfg, "exchange_id")

		stBody := getRaw("/api/strategies/" + sid)
		var st map[string]any
		_ = json.Unmarshal(stBody, &st)
		if c, ok := st["config"].(map[string]any); ok {
			cc, _ := c["copy_config"].(map[string]any)
			if cc != nil {
				r.leader = firstStr(cc, "leader_address")
				r.layer = firstStr(cc, "copy_layer")
			}
		}

		fullBody := getRaw("/api/positions/history?trader_id=" + id + "&limit=500")
		var hist struct {
			Stats json.RawMessage `json:"stats"`
		}
		if err := json.Unmarshal(fullBody, &hist); err == nil && len(hist.Stats) > 0 && string(hist.Stats) != "null" {
			var s map[string]any
			if json.Unmarshal(hist.Stats, &s) == nil {
				r.stats = s
			}
		}

		acct := getRaw("/api/account?trader_id=" + id)
		var a map[string]any
		_ = json.Unmarshal(acct, &a)
		r.equity = toF(a["total_equity"], a["equity"])

		posBody := getRaw("/api/positions?trader_id=" + id)
		var posWrap map[string]any
		_ = json.Unmarshal(posBody, &posWrap)
		var positions []any
		for _, key := range []string{"positions", "items", "positions_arr"} {
			if p, ok := posWrap[key].([]any); ok {
				positions = p
				break
			}
		}
		for _, pr := range positions {
			pm, _ := pr.(map[string]any)
			if pm == nil {
				continue
			}
			qty := firstStr(pm, "quantity", "positionAmt", "position_amt")
			if qty == "0" || qty == "" {
				continue
			}
			r.openPositions++
			r.openPnL += toF(pm["unrealized_pnl"])
		}
		rows = append(rows, r)
		fmt.Fprintf(os.Stderr, "  %s exch=%s leader=%s stats=%v\n", name, r.exchange, short(r.leader), r.stats != nil)
	}

	sort.SliceStable(rows, func(i, j int) bool { return totalPnL(rows[i].stats) > totalPnL(rows[j].stats) })

	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := map[string]any{
			"name": r.name, "exchange": r.exchange, "leader": r.leader,
			"layer": r.layer, "running": r.running,
			"equity": r.equity, "open_positions": r.openPositions, "open_pnl": r.openPnL,
			"stats": r.stats,
		}
		out = append(out, m)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	_ = enc.Encode(map[string]any{"generated_at": time.Now().UTC().Format(time.RFC3339), "traders": out})
}

func totalPnL(stats map[string]any) float64 {
	if stats == nil {
		return -1e18
	}
	return toF(stats["total_pnl"])
}

func short(addr string) string {
	if len(addr) > 10 {
		return addr[:8] + "…" + addr[len(addr)-4:]
	}
	return addr
}

func toF(vals ...any) float64 {
	for _, v := range vals {
		switch t := v.(type) {
		case float64:
			return t
		case string:
			var f float64
			if _, err := fmt.Sscanf(strings.TrimSpace(t), "%f", &f); err == nil {
				return f
			}
		case json.Number:
			f, _ := t.Float64()
			return f
		}
	}
	return 0
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if m == nil {
			return ""
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
