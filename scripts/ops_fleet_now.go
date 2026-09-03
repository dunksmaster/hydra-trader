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

const userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "fleet-now@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	c := &http.Client{Timeout: 90 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := c.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}

	fmt.Println("=== Fleet snapshot ===\n")
	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)

	// HL wallet summary from first running HL trader
	for _, tr := range traders {
		name := firstStr(tr, "trader_name")
		id := firstStr(tr, "trader_id")
		running := fmt.Sprint(tr["is_running"]) == "true"
		if !running && !strings.Contains(strings.ToLower(name), "bigg") {
			continue
		}
		fmt.Printf("## %s\n", name)
		fmt.Printf("  running=%v exchange_id=%s\n", running, firstStr(tr, "exchange_id"))

		acct := map[string]any{}
		_ = json.Unmarshal(get("/api/account?trader_id="+id), &acct)
		if len(acct) > 0 {
			fmt.Printf("  equity=$%.2f available=$%.2f upnl=$%.2f pnl_pct=%.2f%%\n",
				num(acct["total_equity"]), num(acct["available_balance"]),
				num(acct["unrealized_profit"]), num(acct["total_pnl_pct"]))
		}

		posRaw := get("/api/positions?trader_id=" + id)
		var positions []any
		if json.Unmarshal(posRaw, &positions) != nil {
			var wrap map[string]any
			_ = json.Unmarshal(posRaw, &wrap)
			positions, _ = wrap["positions"].([]any)
		}
		open := 0
		for _, raw := range positions {
			p, _ := raw.(map[string]any)
			if p == nil {
				continue
			}
			qty := num(firstAny(p, "quantity", "positionAmt", "size"))
			if qty == 0 {
				continue
			}
			open++
			fmt.Printf("  position: %v %v qty=%.6g entry=%v mark=%v upnl=%v lev=%v\n",
				p["symbol"], p["side"], qty, p["entry_price"], p["mark_price"], p["unrealized_pnl"], p["leverage"])
		}
		if open == 0 {
			fmt.Println("  positions: none")
		}

		sid := firstStr(tr, "strategy_id")
		if sid != "" {
			var st map[string]any
			_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
			cfg, _ := st["config"].(map[string]any)
			cc, _ := cfg["copy_config"].(map[string]any)
			if cc != nil {
				leader := firstStr(cc, "leader_address")
				if leader != "" {
					fmt.Printf("  leader=%s…%s copy_paused=%v overflow=%v\n",
						leader[:6], leader[len(leader)-4:], cc["copy_paused"], cc["overflow_enabled"])
				}
			}
		}

		// recent orders
		var ordWrap map[string]any
		_ = json.Unmarshal(get("/api/orders?trader_id="+id+"&limit=5"), &ordWrap)
		orders, _ := ordWrap["orders"].([]any)
		if orders == nil {
			var flat []any
			_ = json.Unmarshal(get("/api/orders?trader_id="+id+"&limit=5"), &flat)
			orders = flat
		}
		if len(orders) > 0 {
			fmt.Println("  recent fills:")
			for i, raw := range orders {
				if i >= 3 {
					break
				}
				o, _ := raw.(map[string]any)
				if o == nil {
					continue
				}
				rsn := truncate(fmt.Sprint(o["reasoning"]), 60)
				fmt.Printf("    %s %s %s qty=%v | %s\n",
					ts(o["filled_at"]), o["symbol"], o["order_action"], o["quantity"], rsn)
			}
		}
		fmt.Println()
	}

	fmt.Println("=== Stopped bots (quick) ===")
	for _, tr := range traders {
		if fmt.Sprint(tr["is_running"]) == "true" {
			continue
		}
		name := firstStr(tr, "trader_name")
		if strings.Contains(strings.ToLower(name), "bigg") {
			fmt.Printf("  %s — stopped (Bitget idle)\n", name)
			continue
		}
		fmt.Printf("  %s — stopped, copy_paused (no activity)\n", name)
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

func firstAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "<nil>" {
			return v
		}
	}
	return nil
}

func num(v any) float64 {
	var f float64
	fmt.Sscan(fmt.Sprint(v), &f)
	return f
}

func ts(v any) string {
	var ms int64
	fmt.Sscan(fmt.Sprint(v), &ms)
	if ms > 1e12 {
		return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
	}
	return fmt.Sprint(v)
}

func truncate(s string, n int) string {
	if s == "" || s == "<nil>" {
		return "-"
	}
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
