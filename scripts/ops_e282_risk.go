//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"nofx/auth"
)

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "risk@local")
	req, _ := http.NewRequest("GET", base+"/api/my-traders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var traders []map[string]any
	json.Unmarshal(b, &traders)
	for _, tr := range traders {
		if !strings.Contains(strings.ToLower(fmt.Sprint(tr["trader_name"])), "e282") {
			continue
		}
		sid := fmt.Sprint(tr["strategy_id"])
		req2, _ := http.NewRequest("GET", base+"/api/strategies/"+sid, nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		r2, _ := http.DefaultClient.Do(req2)
		b2, _ := io.ReadAll(r2.Body)
		r2.Body.Close()
		var st map[string]any
		json.Unmarshal(b2, &st)
		cfg, _ := st["config"].(map[string]any)
		ai, _ := cfg["ai_config"].(map[string]any)
		rc, _ := ai["risk_control"].(map[string]any)
		cc, _ := ai["copy_config"].(map[string]any)
		if cc == nil {
			cc, _ = cfg["copy_config"].(map[string]any)
		}
		fmt.Printf("copy notional=%v\n", cc["notional_usd"])
		fmt.Printf("risk_control=%s\n", must(rc))
	}
}
func must(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
