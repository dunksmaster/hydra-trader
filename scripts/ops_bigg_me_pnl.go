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

const biggID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: "08ab3fcb-8486-45cf-bd27-0ad35443ff61", Email: "me-pnl@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	base := "https://nofx-production-fcd1.up.railway.app"
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			fatal("%v", err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}

	fmt.Println("=== MEUSDT orders (last 30) ===")
	var wrap map[string]any
	_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=200"), &wrap)
	orders, _ := wrap["orders"].([]any)
	if orders == nil {
		var flat []any
		_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=200"), &flat)
		orders = flat
	}
	meOrders := 0
	var mePnL float64
	for _, raw := range orders {
		o, _ := raw.(map[string]any)
		if o == nil || !strings.Contains(strings.ToUpper(fmt.Sprint(o["symbol"])), "MEUSDT") {
			continue
		}
		meOrders++
		if meOrders <= 20 {
			fmt.Printf("  %s %s qty=%v fill=%v price=%v rpnl=%v comm=%v\n",
				ts(o["filled_at"]), o["order_action"], o["quantity"], o["filled_quantity"],
				o["avg_fill_price"], o["realized_pnl"], o["commission"])
		}
		if v := num(o["realized_pnl"]); v != 0 {
			mePnL += v
		}
		if v := num(o["commission"]); v != 0 {
			mePnL += v // commission usually negative
		}
	}
	fmt.Printf("\nMEUSDT rows in ledger: %d | sum(realized_pnl+commission) from rows: %.4f\n", meOrders, mePnL)

	fmt.Println("\n=== Closed positions mentioning ME ===")
	var hist map[string]any
	_ = json.Unmarshal(get("/api/positions/history?trader_id="+biggID+"&limit=50"), &hist)
	closed, _ := hist["positions"].([]any)
	found := 0
	for _, raw := range closed {
		p, _ := raw.(map[string]any)
		if p == nil {
			continue
		}
		sym := strings.ToUpper(fmt.Sprint(p["symbol"]))
		if !strings.Contains(sym, "ME") {
			continue
		}
		found++
		fmt.Printf("  %s %v %v pnl=%v entry=%v exit=%v qty=%v\n",
			ts(p["closed_at"]), p["symbol"], p["side"], p["realized_pnl"],
			p["entry_price"], p["exit_price"], p["quantity"])
	}
	if found == 0 {
		fmt.Println("  (no ME in closed position history)")
	}

	fmt.Println("\n=== Trades endpoint (ME filter) ===")
	var tradesWrap map[string]any
	_ = json.Unmarshal(get("/api/trades?trader_id="+biggID+"&limit=100"), &tradesWrap)
	trades, _ := tradesWrap["trades"].([]any)
	if trades == nil {
		var flat []any
		_ = json.Unmarshal(get("/api/trades?trader_id="+biggID+"&limit=100"), &flat)
		trades = flat
	}
	tfound := 0
	for _, raw := range trades {
		t, _ := raw.(map[string]any)
		if t == nil || !strings.Contains(strings.ToUpper(fmt.Sprint(t["symbol"])), "MEUSDT") {
			continue
		}
		tfound++
		if tfound <= 15 {
			fmt.Printf("  %s %v side=%v qty=%v price=%v pnl=%v\n",
				ts(t["timestamp"]), t["symbol"], t["side"], t["quantity"], t["price"], t["realized_pnl"])
		}
	}
	if tfound == 0 {
		fmt.Println("  (no MEUSDT in trades)")
	}

	fmt.Println("\n=== All MEUSDT order actions ===")
	for _, raw := range orders {
		o, _ := raw.(map[string]any)
		if o == nil || !strings.Contains(strings.ToUpper(fmt.Sprint(o["symbol"])), "MEUSDT") {
			continue
		}
		act := fmt.Sprint(o["order_action"])
		if strings.Contains(act, "close") || strings.Contains(strings.ToLower(act), "close") {
			fmt.Printf("  CLOSE %s %s qty=%v price=%v rpnl=%v\n", ts(o["filled_at"]), act, o["quantity"], o["avg_fill_price"], o["realized_pnl"])
		}
	}

	fmt.Println("\n=== Latest 8 decisions (newest) ===")
	var dec []map[string]any
	_ = json.Unmarshal(get("/api/decisions/latest?trader_id="+biggID+"&limit=8"), &dec)
	if len(dec) == 0 {
		var wrap2 map[string]any
		_ = json.Unmarshal(get("/api/decisions/latest?trader_id="+biggID+"&limit=8"), &wrap2)
		if arr, ok := wrap2["decisions"].([]any); ok {
			for _, x := range arr {
				if m, ok := x.(map[string]any); ok {
					dec = append(dec, m)
				}
			}
		}
	}
	for _, d := range dec {
		fmt.Printf("\n--- %v cycle=%v ---\n", d["timestamp"], d["cycle_number"])
		fmt.Printf("decision_json: %s\n", truncate(fmt.Sprint(d["decision_json"]), 250))
		fmt.Printf("execution_log: %s\n", truncate(fmt.Sprint(d["execution_log"]), 250))
		if decs, ok := d["decisions"].([]any); ok {
			for _, x := range decs {
				m, _ := x.(map[string]any)
				if m != nil {
					fmt.Printf("  exec: %v %v success=%v\n", m["symbol"], m["action"], m["success"])
				}
			}
		}
	}

	// Estimate ME round-trip from known fills if long then short same size
	fmt.Println("\n=== Estimated ME round-trip (from fill prices) ===")
	longQty, shortQty := 0.0, 0.0
	longCost, shortCost := 0.0, 0.0
	longComm, shortComm := 0.0, 0.0
	for _, raw := range orders {
		o, _ := raw.(map[string]any)
		if o == nil || !strings.Contains(strings.ToUpper(fmt.Sprint(o["symbol"])), "MEUSDT") {
			continue
		}
		qty := num(o["filled_quantity"])
		if qty == 0 {
			qty = num(o["quantity"])
		}
		price := num(o["avg_fill_price"])
		comm := num(o["commission"])
		act := strings.ToLower(fmt.Sprint(o["order_action"]))
		switch {
		case strings.Contains(act, "open_long"):
			longQty += qty
			longCost += qty * price
			longComm += comm
		case strings.Contains(act, "open_short"):
			shortQty += qty
			shortCost += qty * price
			shortComm += comm
		}
	}
	if longQty > 0 {
		avgLong := longCost / longQty
		avgShort := 0.0
		if shortQty > 0 {
			avgShort = shortCost / shortQty
		}
		// If flip: long closed at ~short entry
		flipPnL := (avgShort - avgLong) * longQty
		totalComm := longComm + shortComm
		fmt.Printf("  Long: qty=%.0f avg=%.5f | Short open: qty=%.0f avg=%.5f\n", longQty, avgLong, shortQty, avgShort)
		fmt.Printf("  Est. long leg PnL (exit at short entry): %.4f USDT\n", flipPnL)
		fmt.Printf("  Commissions on opens: %.4f USDT\n", totalComm)
		fmt.Printf("  Est. long+fees if flat after flip at same price: %.4f USDT\n", flipPnL+totalComm)
		fmt.Println("  (Short leg close PnL unknown — no close_* rows in NOFX ledger)")
	}

	fmt.Println("\n=== Decisions with ME or execution (last 20) ===")
	var decOld []map[string]any
	_ = json.Unmarshal(get("/api/decisions?trader_id="+biggID+"&limit=20"), &decOld)
	for _, d := range decOld {
		tsStr := fmt.Sprint(d["timestamp"])
		coins := fmt.Sprint(d["candidate_coins"])
		elog := fmt.Sprint(d["execution_log"])
		djson := fmt.Sprint(d["decision_json"])
		if !strings.Contains(coins, "ME") && !strings.Contains(djson, "ME") && !strings.Contains(elog, "ME") {
			continue
		}
		fmt.Printf("\n--- %s cycle=%v ---\n", tsStr, d["cycle_number"])
		fmt.Printf("execution_log: %s\n", truncate(elog, 200))
		fmt.Printf("decision_json: %s\n", truncate(djson, 300))
		if decs, ok := d["decisions"].([]any); ok {
			for _, x := range decs {
				m, _ := x.(map[string]any)
				if m == nil {
					continue
				}
				fmt.Printf("  exec: %v %v success=%v err=%v\n", m["symbol"], m["action"], m["success"], m["error"])
			}
		}
	}
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
		return time.UnixMilli(ms).UTC().Format("15:04 UTC")
	}
	return fmt.Sprint(v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
