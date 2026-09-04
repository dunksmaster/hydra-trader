//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nofx/auth"
	"os"
	"strings"
)

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "detail@local")
	traderID := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	req, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/decisions/latest?trader_id="+traderID, nil)
	req.Header.Set("Authorization", "Bearer "+t)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var latest []map[string]any
	json.Unmarshal(b, &latest)
	if len(latest) == 0 {
		fmt.Println("no decisions")
		return
	}
	d := latest[0]
	fmt.Printf("timestamp=%v\n", d["timestamp"])
	fmt.Printf("ai_call_duration_sec=%v\n", d["ai_call_duration_sec"])
	fmt.Printf("error_message=%v\n", d["error_message"])
	reasoning, _ := d["reasoning"].(string)
	fmt.Printf("reasoning_len=%d\n", len(reasoning))
	lower := strings.ToLower(reasoning)
	fmt.Printf("has_claw402=%v has_tf=%v has_ema=%v has_rsi=%v\n",
		strings.Contains(lower, "signal lab") || strings.Contains(lower, "heatmap") || strings.Contains(lower, "claw402"),
		strings.Contains(lower, "5m") || strings.Contains(lower, "15m") || strings.Contains(lower, "1h") || strings.Contains(lower, "4h") || strings.Contains(lower, "timeframe"),
		strings.Contains(lower, "ema"), strings.Contains(lower, "rsi"))
	if len(reasoning) > 2000 {
		reasoning = reasoning[:2000] + "..."
	}
	fmt.Printf("reasoning=\n%s\n", reasoning)
	if decs, ok := d["decisions"].([]any); ok {
		fmt.Printf("decisions_count=%d\n", len(decs))
		for i, dr := range decs {
			dm, _ := dr.(map[string]any)
			fmt.Printf("  [%d] symbol=%v action=%v confidence=%v\n", i, dm["symbol"], dm["action"], dm["confidence"])
		}
	}
}
