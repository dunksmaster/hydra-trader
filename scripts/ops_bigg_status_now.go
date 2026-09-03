//go:build ignore

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

	"github.com/golang-jwt/jwt/v5"
)

const (
	userID   = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggID   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	strategy = "b723efa8-729d-47cd-a71e-99429c639b6a"
)

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "bigg-status@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("GET %s err: %v\n", path, err)
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b
	}

	var cfg map[string]any
	_ = json.Unmarshal(get("/api/traders/"+biggID+"/config"), &cfg)
	fmt.Printf("=== Crypto BigG status ===\n")
	fmt.Printf("running=%v scan=%v min model=%v\n", cfg["is_running"], cfg["scan_interval_minutes"], cfg["ai_model"])

	acct := map[string]any{}
	_ = json.Unmarshal(get("/api/account?trader_id="+biggID), &acct)
	fmt.Printf("\nAccount: equity=$%.2f available=$%.2f pnl=$%.2f (%.2f%%)\n",
		num(acct["total_equity"]), num(acct["available_balance"]), num(acct["total_pnl"]), num(acct["total_pnl_pct"]))

	posWrap := map[string]any{}
	_ = json.Unmarshal(get("/api/positions?trader_id="+biggID), &posWrap)
	positions, _ := posWrap["positions"].([]any)
	if positions == nil {
		var flat []any
		_ = json.Unmarshal(get("/api/positions?trader_id="+biggID), &flat)
		positions = flat
	}
	fmt.Printf("\nOpen positions: %d\n", len(positions))
	for _, raw := range positions {
		p, _ := raw.(map[string]any)
		if p == nil {
			continue
		}
		fmt.Printf("  %v %v qty=%v entry=%v upnl=%v lev=%v\n",
			p["symbol"], p["side"], p["quantity"], p["entry_price"], p["unrealized_pnl"], p["leverage"])
	}

	var st map[string]any
	_ = json.Unmarshal(get("/api/strategies/"+strategy), &st)
	conf, _ := st["config"].(map[string]any)
	ai, _ := conf["ai_config"].(map[string]any)
	cs, _ := ai["coin_source"].(map[string]any)
	fmt.Printf("\nStrategy board: %v / %v / %v / limit %v\n",
		cs["source_type"], cs["hyper_rank_category"], cs["hyper_rank_direction"], cs["hyper_rank_limit"])

	fmt.Println("\n=== Closed positions (10) ===")
	var hist map[string]any
	_ = json.Unmarshal(get("/api/positions/history?trader_id="+biggID+"&limit=10"), &hist)
	closed, _ := hist["positions"].([]any)
	for _, raw := range closed {
		p, _ := raw.(map[string]any)
		if p == nil {
			continue
		}
		fmt.Printf("  %s %v %v pnl=%v entry=%v exit=%v\n",
			formatTS(p["closed_at"]), p["symbol"], p["side"], p["realized_pnl"], p["entry_price"], p["exit_price"])
	}

	fmt.Println("\n=== Latest AI decisions (5) ===")
	var decWrap map[string]any
	_ = json.Unmarshal(get("/api/decisions/latest?trader_id="+biggID+"&limit=5"), &decWrap)
	decisions, _ := decWrap["decisions"].([]any)
	if decisions == nil {
		_ = json.Unmarshal(get("/api/decisions/latest?trader_id="+biggID+"&limit=5"), &decisions)
	}
	if len(decisions) == 0 {
		fmt.Println("  (no decision records yet)")
	}
	for _, raw := range decisions {
		d, _ := raw.(map[string]any)
		if d == nil {
			continue
		}
		ts := formatTS(firstAny(d, "created_at", "timestamp"))
		action := firstStr(d, "action", "decision")
		sym := firstStr(d, "symbol")
		confidence := d["confidence"]
		reasoning := truncate(fmt.Sprint(firstAny(d, "reasoning", "summary")), 120)
		fmt.Printf("  %s %s %s conf=%v\n    %s\n", ts, sym, action, confidence, reasoning)
	}

	fmt.Println("\n=== Recent NOFX orders (15) ===")
	var ordWrap map[string]any
	_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=100"), &ordWrap)
	orders, _ := ordWrap["orders"].([]any)
	if orders == nil {
		var flat []any
		_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=100"), &flat)
		orders = flat
	}
	for i, raw := range orders {
		if i >= 15 {
			break
		}
		o, _ := raw.(map[string]any)
		if o == nil {
			continue
		}
		fmt.Printf("  %s %s %s qty=%v rpnl=%v | %s\n",
			formatTS(o["filled_at"]), o["symbol"], o["order_action"], o["quantity"], o["realized_pnl"],
			truncate(fmt.Sprint(o["reasoning"]), 80))
	}

	fmt.Println("\n=== Today's session summary ===")
	today := time.Now().UTC().Format("2006-01-02")
	opens, closes := 0, 0
	syms := map[string]int{}
	for _, raw := range orders {
		o, _ := raw.(map[string]any)
		if o == nil {
			continue
		}
		ts := formatTS(o["filled_at"])
		if !strings.HasPrefix(ts, today) {
			continue
		}
		act := strings.ToLower(fmt.Sprint(o["order_action"]))
		sym := fmt.Sprint(o["symbol"])
		syms[sym]++
		if strings.Contains(act, "close") {
			closes++
		} else if strings.Contains(act, "open") {
			opens++
		}
	}
	fmt.Printf("  UTC date %s: %d opens, %d closes (from last 100 ledger rows)\n", today, opens, closes)
	for sym, n := range syms {
		fmt.Printf("    %s: %d order events\n", sym, n)
	}
}

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	default:
		var f float64
		fmt.Sscan(fmt.Sprint(v), &f)
		return f
	}
}

func formatTS(v any) string {
	ms := int64(num(v))
	if ms > 1e12 {
		return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
	}
	if ms > 0 {
		return time.Unix(ms, 0).UTC().Format("2006-01-02 15:04 UTC")
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func firstAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "" && fmt.Sprint(v) != "<nil>" {
			return v
		}
	}
	return nil
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
