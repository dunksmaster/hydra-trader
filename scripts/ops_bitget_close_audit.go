//go:build ignore

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
	baseURL := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "bitget-close-audit@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	if err != nil {
		fmt.Println("token error:", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("ERR", path, err)
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			fmt.Printf("HTTP %d %s\n", resp.StatusCode, path)
		}
		return b
	}

	raw := get("/api/my-traders")
	var traders []map[string]any
	_ = json.Unmarshal(raw, &traders)
	if items, ok := parseItems(raw); ok {
		traders = items
	}

	var bitgetIDs []string
	fmt.Println("=== Bitget traders ===")
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		ex := strings.ToLower(firstStr(tr, "exchange", "exchange_type"))
		if !strings.Contains(ex, "bitget") && !strings.Contains(strings.ToLower(name), "bigg") {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		bitgetIDs = append(bitgetIDs, id)
		fmt.Printf("- %s id=%s running=%v exchange=%s\n", name, id, tr["is_running"], ex)
	}

	for _, id := range bitgetIDs {
		fmt.Printf("\n=== Open positions trader=%s ===\n", id)
		posRaw := get("/api/positions?trader_id=" + id)
		if items, ok := parseItems(posRaw); ok {
			for _, p := range items {
				fmt.Printf("  OPEN %s %s qty=%s entry=%s pnl=%s\n",
					str(p["symbol"]), str(p["side"]), str(p["quantity"]), str(p["entry_price"]), str(p["unrealized_pnl"]))
			}
		} else {
			var wrap map[string]any
			_ = json.Unmarshal(posRaw, &wrap)
			if pos, ok := wrap["positions"].([]any); ok {
				for _, it := range pos {
					p, _ := it.(map[string]any)
					fmt.Printf("  OPEN %s %s qty=%s entry=%s pnl=%s\n",
						str(p["symbol"]), str(p["side"]), str(p["quantity"]), str(p["entry_price"]), str(p["unrealized_pnl"]))
				}
			}
		}

		fmt.Printf("\n=== Recent orders/fills (Bitget) trader=%s ===\n", id)
		ordRaw := get("/api/orders?trader_id=" + id + "&limit=50")
		var orders []map[string]any
		if items, ok := parseItems(ordRaw); ok {
			orders = items
		} else {
			_ = json.Unmarshal(ordRaw, &orders)
		}
		closeN, openN := 0, 0
		for _, o := range orders {
			act := strings.ToLower(str(o["order_action"]))
			if act == "" {
				act = strings.ToLower(str(o["action"]))
			}
			isClose := strings.Contains(act, "close")
			if isClose {
				closeN++
			} else {
				openN++
			}
			tag := "OPEN"
			if isClose {
				tag = "CLOSE"
			}
			fmt.Printf("  [%s] %s %s action=%s qty=%s pnl=%s time=%s id=%s\n",
				tag, str(o["symbol"]), str(o["side"]), act, str(o["quantity"]), str(o["realized_pnl"]),
				str(o["filled_at"]), str(o["exchange_order_id"]))
		}
		fmt.Printf("  → %d opens, %d closes in store (last 50)\n", openN, closeN)
		if len(orders) == 0 {
			fmt.Println("  (no orders in store)")
		}

		fmt.Printf("\n=== Closed history (Bitget) trader=%s ===\n", id)
		histRaw := get("/api/positions/history?trader_id=" + id + "&limit=15")
		var hist []map[string]any
		if items, ok := parseItems(histRaw); ok {
			hist = items
		} else {
			_ = json.Unmarshal(histRaw, &hist)
		}
		sort.Slice(hist, func(i, j int) bool {
			return str(hist[i]["exit_time"]) > str(hist[j]["exit_time"])
		})
		for _, h := range hist {
			fmt.Printf("  %s %s exit=%s pnl=%s reason=%s\n",
				str(h["symbol"]), str(h["side"]), str(h["exit_time"]), str(h["realized_pnl"]), str(h["close_reason"]))
		}
		if len(hist) == 0 {
			fmt.Println("  (no closed rows in store)")
		}

		fmt.Printf("\n=== Close decisions (store) trader=%s ===\n", id)
		decRaw := get("/api/decisions?trader_id=" + id + "&limit=50")
		var decs []map[string]any
		if items, ok := parseItems(decRaw); ok {
			decs = items
		} else {
			_ = json.Unmarshal(decRaw, &decs)
		}
		closeCount := 0
		for _, d := range decs {
			act := strings.ToLower(str(d["action"]))
			if !strings.HasPrefix(act, "close_") {
				continue
			}
			closeCount++
			fmt.Printf("  %s %s @ %s reasoning=%s\n",
				str(d["symbol"]), act, str(d["created_at"]), truncate(str(d["reasoning"]), 120))
		}
		if closeCount == 0 {
			fmt.Println("  (no close decisions logged)")
		}
	}

	fmt.Println("\n=== Copy bots with overflow (leader-driven closes) ===")
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		sid := firstStr(tr, "strategy_id")
		if sid == "" {
			continue
		}
		stRaw := get("/api/strategies/" + sid)
		var st map[string]any
		_ = json.Unmarshal(stRaw, &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil || !truthy(cc["overflow_enabled"]) {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		fmt.Printf("- %s leader=%s overflow→%v running=%v\n",
			name, str(cc["leader_address"]), cc["overflow_trader_id"], tr["is_running"])
		decRaw := get("/api/decisions?trader_id=" + id + "&limit=30")
		var decs []map[string]any
		if items, ok := parseItems(decRaw); ok {
			decs = items
		} else {
			_ = json.Unmarshal(decRaw, &decs)
		}
		for _, d := range decs {
			act := strings.ToLower(str(d["action"]))
			if !strings.HasPrefix(act, "close_") {
				continue
			}
			r := str(d["reasoning"])
			if strings.Contains(r, "overflow") || strings.Contains(r, "leader") || strings.Contains(r, "copy") {
				fmt.Printf("    HL copy close: %s %s — %s\n", str(d["symbol"]), act, truncate(r, 100))
			}
		}
	}
}

func parseItems(raw []byte) ([]map[string]any, bool) {
	var wrap map[string]any
	if json.Unmarshal(raw, &wrap) != nil {
		return nil, false
	}
	if items, ok := wrap["items"].([]any); ok {
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out, true
	}
	return nil, false
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

func str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	case float64:
		return t != 0
	default:
		return false
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
