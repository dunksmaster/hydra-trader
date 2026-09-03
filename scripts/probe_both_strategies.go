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
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "probe@local")
	base := "https://nofx-production-fcd1.up.railway.app"
	get := func(p string) []byte {
		req, _ := http.NewRequest("GET", base+p, nil)
		req.Header.Set("Authorization", "Bearer "+t)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b
	}

	var traders []any
	json.Unmarshal(get("/api/my-traders"), &traders)
	for _, item := range traders {
		tr, _ := item.(map[string]any)
		name := fmt.Sprint(tr["trader_name"])
		sid, _ := tr["strategy_id"].(string)
		fmt.Printf("\n===== %v =====\ntrader_id=%v\nstrategy_id=%v exchange=%v scan=%v running=%v\n",
			name, tr["trader_id"], sid, tr["exchange"], tr["scan_interval_minutes"], tr["is_running"])
		if sid == "" {
			continue
		}
		var s map[string]any
		json.Unmarshal(get("/api/strategies/"+sid), &s)
		cfg, _ := s["config"].(map[string]any)
		ai, _ := cfg["ai_config"].(map[string]any)
		if ai == nil {
			ai = cfg
		}
		fmt.Printf("strategy_name=%v\n", s["name"])
		fmt.Printf("risk=%s\n", jsonPretty(ai["risk_control"]))
		fmt.Printf("coin_source=%s\n", jsonPretty(ai["coin_source"]))
		if ind, ok := ai["indicators"].(map[string]any); ok {
			fmt.Printf("klines=%s\n", jsonPretty(ind["klines"]))
		}
		if ps, ok := ai["prompt_sections"].(map[string]any); ok {
			keys := make([]string, 0, len(ps))
			for k := range ps {
				keys = append(keys, k)
			}
			fmt.Printf("prompt_section_keys=%v\n", keys)
			for _, k := range []string{"trading_frequency", "entry_standards", "risk_management", "exit_rules"} {
				if v, ok := ps[k].(string); ok && v != "" {
					fmt.Printf("--- %s ---\n%s\n", k, v)
				}
			}
		}
	}
}

func jsonPretty(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
