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
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "verify@local")
	base := "https://nofx-production-fcd1.up.railway.app"
	traderID := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"

	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Authorization", "Bearer "+t)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b, resp.StatusCode
	}

	body, status := get("/api/traders/" + traderID + "/config")
	fmt.Printf("trader_config status=%d\n", status)
	var cfg map[string]any
	json.Unmarshal(body, &cfg)
	fmt.Printf("trader=%v running=%v scan_min=%v strategy=%v\n",
		cfg["trader_name"], cfg["is_running"], cfg["scan_interval_minutes"], cfg["strategy_id"])

	body, status = get("/api/decisions/latest?trader_id=" + traderID)
	fmt.Printf("latest_decisions status=%d\n", status)
	var latest []any
	json.Unmarshal(body, &latest)
	fmt.Printf("latest_decisions_count=%d\n", len(latest))
	for i, raw := range latest {
		if i >= 3 {
			break
		}
		d, _ := raw.(map[string]any)
		ts, _ := d["timestamp"].(string)
		dur, _ := d["ai_call_duration_sec"].(float64)
		reasoning, _ := d["reasoning"].(string)
		lower := strings.ToLower(reasoning)
		hasClaw402 := strings.Contains(lower, "signal lab") || strings.Contains(lower, "heatmap") || strings.Contains(lower, "claw402")
		hasTF := strings.Contains(lower, "5m") || strings.Contains(lower, "15m") || strings.Contains(lower, "1h") || strings.Contains(lower, "4h") || strings.Contains(lower, "timeframe")
		if len(reasoning) > 500 {
			reasoning = reasoning[:500] + "..."
		}
		fmt.Printf("--- decision[%d] ts=%s ai_duration=%.2fs claw402_refs=%v tf_refs=%v ---\n%s\n",
			i, ts, dur, hasClaw402, hasTF, reasoning)
		if decs, ok := d["decisions"].([]any); ok {
			for j, dr := range decs {
				if j >= 6 {
					break
				}
				dm, _ := dr.(map[string]any)
				fmt.Printf("  [%d] %v %v\n", j, dm["symbol"], dm["action"])
			}
		}
	}

	body, status = get("/api/decisions?trader_id=" + traderID + "&limit=5")
	fmt.Printf("decisions status=%d\n", status)
	var records []any
	json.Unmarshal(body, &records)
	fmt.Printf("decision_records_count=%d\n", len(records))

	fmt.Printf("now_utc=%s\n", time.Now().UTC().Format(time.RFC3339))
}
