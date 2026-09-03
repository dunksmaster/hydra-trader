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
		UserID: userID, Email: "bigg-orders@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
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
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		if !strings.Contains(strings.ToLower(name), "bigg") {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		fmt.Printf("=== %s ===\n", name)
		fmt.Printf("running=%v strategy_id=%s exchange_id=%s\n", tr["is_running"], tr["strategy_id"], tr["exchange_id"])

		acct := map[string]any{}
		_ = json.Unmarshal(get("/api/account?trader_id="+id), &acct)
		fmt.Printf("equity=%v available=%v pnl=%v\n", acct["total_equity"], acct["available_balance"], acct["total_pnl"])

		posRaw := map[string]any{}
		_ = json.Unmarshal(get("/api/positions?trader_id="+id), &posRaw)
		positions, _ := posRaw["positions"].([]any)
		fmt.Printf("open positions: %d\n", len(positions))
		for _, p := range positions {
			m, _ := p.(map[string]any)
			if m == nil {
				continue
			}
			qty := firstStr(m, "quantity", "positionAmt", "position_amt")
			if qty == "0" || qty == "" {
				continue
			}
			fmt.Printf("  • %s %s qty=%s entry=%v upnl=%v\n", m["symbol"], m["side"], qty, m["entry_price"], m["unrealized_pnl"])
		}

		ordRaw := map[string]any{}
		_ = json.Unmarshal(get("/api/orders?trader_id="+id+"&limit=100"), &ordRaw)
		orders, _ := ordRaw["orders"].([]any)
		if orders == nil {
			// flat array fallback
			var flat []map[string]any
			_ = json.Unmarshal(get("/api/orders?trader_id="+id+"&limit=100"), &flat)
			for _, m := range flat {
				orders = append(orders, m)
			}
		}
		symbols := map[string]bool{}
		openCount := 0
		for _, o := range orders {
			m, _ := o.(map[string]any)
			if m == nil {
				continue
			}
			sym := firstStr(m, "symbol")
			if sym != "" {
				symbols[sym] = true
			}
			st := strings.ToLower(firstStr(m, "status"))
			if st != "open" && st != "new" && st != "partially_filled" && st != "live" && st != "pending" {
				continue
			}
			openCount++
			fmt.Printf("  • ledger order %s %s qty=%v status=%s\n",
				m["symbol"], m["side"], m["quantity"], m["status"])
		}
		fmt.Printf("open/pending in NOFX ledger: %d\n", openCount)

		// Live exchange open orders (requires symbol; probe recent symbols + common).
		probe := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "HYPEUSDT"}
		for s := range symbols {
			probe = append(probe, s)
		}
		seenSym := map[string]bool{}
		exOpen := 0
		fmt.Println("live Bitget open orders (exchange API):")
		for _, sym := range probe {
			if seenSym[sym] {
				continue
			}
			seenSym[sym] = true
			raw := get("/api/open-orders?trader_id=" + id + "&symbol=" + sym)
			var oo []map[string]any
			if json.Unmarshal(raw, &oo) == nil && len(oo) > 0 {
				for _, m := range oo {
					exOpen++
					fmt.Printf("  • %s %s qty=%v type=%v price=%v\n", m["symbol"], m["side"], m["quantity"], m["type"], m["price"])
				}
			}
		}
		if exOpen == 0 {
			fmt.Println("  (none found on probed symbols — check Bitget UI product tab: UTA vs spot)")
		}
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
