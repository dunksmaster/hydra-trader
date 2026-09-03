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
)

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "status@local")
	base := "https://nofx-production-fcd1.up.railway.app"
	h := http.DefaultClient

	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Authorization", "Bearer "+t)
		resp, err := h.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b, resp.StatusCode
	}

	for _, id := range []struct {
		id, name string
	}{
		{"8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332", "Crypto BigG"},
		{"8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502", "NOFX Autopilot"},
	} {
		fmt.Printf("\n========== %s ==========\n", id.name)
		var cfg map[string]any
		b, st := get("/api/traders/" + id.id + "/config")
		fmt.Printf("config status=%d\n", st)
		json.Unmarshal(b, &cfg)
		fmt.Printf("running=%v scan_min=%v\n", cfg["is_running"], cfg["scan_interval_minutes"])

		var acct map[string]any
		b, _ = get("/api/account?trader_id=" + id.id)
		json.Unmarshal(b, &acct)
		fmt.Printf("equity=%v available=%v total_pnl_pct=%v margin=%v\n",
			acct["total_equity"], acct["available_balance"], acct["total_pnl_pct"], acct["margin_used_pct"])

		var positions []map[string]any
		b, _ = get("/api/positions?trader_id=" + id.id)
		json.Unmarshal(b, &positions)
		fmt.Printf("open_positions=%d\n", len(positions))
		for i, p := range positions {
			fmt.Printf("  pos[%d] %v %v size=%v entry=%v uPnL=%v lev=%vx\n",
				i, p["symbol"], p["side"], p["position_amt"], p["entry_price"], p["unrealized_pnl"], p["leverage"])
		}

		var latest []map[string]any
		b, _ = get("/api/decisions/latest?trader_id=" + id.id + "&limit=1")
		json.Unmarshal(b, &latest)
		if len(latest) > 0 {
			d := latest[0]
			fmt.Printf("latest_cycle=%v success=%v ai_ms=%v\n", d["timestamp"], d["success"], d["ai_request_duration_ms"])
			if em, ok := d["error_message"].(string); ok && em != "" {
				fmt.Printf("error=%v\n", em)
			}
			if cot, ok := d["cot_trace"].(string); ok && len(cot) > 0 {
				s := cot
				if len(s) > 300 {
					s = s[:300] + "..."
				}
				fmt.Printf("reasoning=%q\n", s)
			}
			if decs, ok := d["decisions"].([]any); ok {
				for j, dr := range decs {
					if j >= 5 {
						break
					}
					dm, _ := dr.(map[string]any)
					fmt.Printf("  decision[%d] %v %v\n", j, dm["symbol"], dm["action"])
				}
			}
			if cands, ok := d["candidate_coins"].([]any); ok {
				names := make([]string, 0, len(cands))
				for _, c := range cands {
					names = append(names, fmt.Sprint(c))
				}
				fmt.Printf("candidates=%v\n", strings.Join(names, ", "))
			}
		}
	}
	fmt.Printf("\nnow_utc=%s\n", time.Now().UTC().Format(time.RFC3339))
}
