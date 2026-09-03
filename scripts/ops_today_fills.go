//go:build ignore

// Deep dive on today's copy trades: which bot executed, leader attribution,
// and per-bot round-trip PnL for the last 24h from NOFX order/position ledger.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	baseURL := "https://nofx-production-fcd1.up.railway.app"
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "today-fills@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}
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

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)

	type summary struct {
		bot      string
		leader   string
		orders   int
		fills    int
		volume   float64
		fees     float64
		opened   int
		closed   int
		realized float64
		coins    map[string]int
	}

	sums := map[string]*summary{}
	order := []string{}

	for _, tr := range traders {
		id := fs(tr, "trader_id", "id")
		name := fs(tr, "trader_name", "name")
		if id == "" || name == "" {
			continue
		}
		// orders from DB ledger (works even for stopped bots)
		ob := get("/api/orders?trader_id=" + id + "&limit=500")
		var orders []map[string]any
		_ = json.Unmarshal(ob, &orders)
		if len(orders) == 0 {
			continue
		}
		s := &summary{bot: name, coins: map[string]int{}}
		// resolve leader
		if sid := fs(tr, "strategy_id"); sid != "" && sid != "<nil>" {
			var st map[string]any
			_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
			if c, ok := st["config"].(map[string]any); ok {
				if cc, ok := c["copy_config"].(map[string]any); ok {
					s.leader = fs(cc, "leader_address")
				}
			}
		}
		for _, o := range orders {
			ts := toF(o["created_at"], o["time"], o["timestamp"])
			// orders endpoint returns ms or s epoch or RFC3339; normalize
			t := parseTime(ts, fs(o, "created_at", "time", "timestamp"))
			if t != nil && time.Since(*t) > 7*24*time.Hour+2*time.Hour {
				continue
			}
			s.orders++
			s.volume += toF(o["quote_quantity"], o["notional"], o["value"])
			s.fees += toF(o["fee"], o["commission"])
			symbol := fs(o, "symbol")
			side := strings.ToLower(fs(o, "side", "action"))
			s.coins[symbol]++
			if strings.Contains(side, "open") || strings.Contains(side, "buy") && !strings.Contains(side, "close") {
				s.opened++
			}
			if strings.Contains(side, "close") || strings.Contains(side, "sell") {
				s.closed++
			}
		}
		// realized pnl from position history (today)
		pb := get("/api/positions/history?trader_id=" + id + "&limit=100")
		var hist struct {
			Positions []map[string]any `json:"positions"`
		}
		_ = json.Unmarshal(pb, &hist)
		for _, p := range hist.Positions {
			t := parseTime(toF(p["closed_at"], p["close_time"]), fs(p, "closed_at", "close_time", "updated_at"))
			if t != nil && time.Since(*t) > 7*24*time.Hour+2*time.Hour {
				continue
			}
			s.realized += toF(p["realized_pnl"], p["pnl"], p["profit"])
			if toF(p["realized_pnl"], p["pnl"], p["profit"]) != 0 {
				s.closed++
			}
		}
		sums[name] = s
		order = append(order, name)
	}

	sort.Slice(order, func(i, j int) bool { return sums[order[i]].orders > sums[order[j]].orders })
	out := map[string]any{"window": "last 7 days", "bots": order}
	list := []map[string]any{}
	for _, name := range order {
		s := sums[name]
		coins := map[string]any{}
		for c, n := range s.coins {
			coins[c] = n
		}
		list = append(list, map[string]any{
			"bot": s.bot, "leader": s.leader, "orders": s.orders,
			"volume_usd": round2(s.volume), "fees_usd": round2(s.fees),
			"opens": s.opened, "closes": s.closed,
			"realized_pnl_today": round2(s.realized), "coins": coins,
		})
	}
	out["detail"] = list
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	_ = enc.Encode(out)
}

func fs(m map[string]any, keys ...string) string {
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

func toF(vals ...any) float64 {
	for _, v := range vals {
		switch t := v.(type) {
		case float64:
			return t
		case json.Number:
			f, _ := t.Float64()
			return f
		case string:
			var f float64
			if _, err := fmt.Sscanf(t, "%f", &f); err == nil {
				return f
			}
		}
	}
	return 0
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func parseTime(num float64, raw string) *time.Time {
	if num > 1e12 { // ms
		t := time.UnixMilli(int64(num))
		return &t
	}
	if num > 1e9 { // s
		t := time.Unix(int64(num), 0)
		return &t
	}
	if raw != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, raw); err == nil {
				return &t
			}
		}
	}
	return nil
}
