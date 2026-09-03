//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"nofx/auth"
)

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	t, err := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "quality@local")
	if err != nil {
		panic(err)
	}
	base := "https://nofx-production-fcd1.up.railway.app"
	get := func(path string) []byte {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Authorization", "Bearer "+t)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			fmt.Printf("  (status=%d on %s)\n", resp.StatusCode, path)
		}
		return b
	}

	for _, bot := range []struct{ id, name string }{
		{"8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332", "Crypto BigG (Bitget)"},
		{"8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502", "NOFX Autopilot (Hyperliquid)"},
	} {
		fmt.Printf("\n========== %s ==========\n", bot.name)

		var full map[string]any
		json.Unmarshal(get("/api/statistics/full?trader_id="+bot.id), &full)
		fmt.Printf("trades=%v win=%v loss=%v win_rate=%v%%\n",
			full["total_trades"], full["win_trades"], full["loss_trades"], full["win_rate"])
		fmt.Printf("net_pnl=%v fees=%v profit_factor=%v avg_win=%v avg_loss=%v maxDD=%v%%\n",
			full["total_pnl"], full["total_fee"], full["profit_factor"],
			full["avg_win"], full["avg_loss"], full["max_drawdown_pct"])

		var hist []map[string]any
		json.Unmarshal(get("/api/positions/history?trader_id="+bot.id+"&limit=40"), &hist)
		fmt.Printf("closed_records=%d\n", len(hist))
		for i, h := range hist {
			if i >= 25 {
				break
			}
			fmt.Printf("  %-10v %-6v pnl=%-10v fee=%-9v held=%vm entry=%v exit=%v\n",
				h["symbol"], h["side"], round2(h["realized_pnl"]), round2(h["total_fee"]),
				heldMinutes(h), h["entry_price"], h["exit_price"])
		}
	}
}

func round2(v any) string {
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%.3f", f)
	}
	return fmt.Sprint(v)
}

func heldMinutes(h map[string]any) string {
	e, _ := h["entry_time"].(float64)
	x, _ := h["exit_time"].(float64)
	if e <= 0 || x <= 0 {
		return "?"
	}
	return fmt.Sprintf("%.0f", (x-e)/60000)
}
