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
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "status@local")
	client := &http.Client{}

	get := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return out
	}

	traders := get("/api/traders")
	for _, raw := range traders["traders"].([]any) {
		t := raw.(map[string]any)
		name := fmt.Sprint(t["name"], t["trader_name"])
		id := fmt.Sprint(t["trader_id"], t["id"])
		fmt.Printf("\n=== %s ===\n", name)
		fmt.Printf("id=%s running=%v scan=%v exchange=%v strategy_id=%v\n", id, t["is_running"], t["scan_interval_minutes"], t["exchange_id"], t["strategy_id"])
		if fmt.Sprint(t["is_running"]) == "true" {
			st := get("/api/status?trader_id=" + id)
			fmt.Printf("status: strategy_type=%v equity=%v positions=%v scan=%v\n", st["strategy_type"], st["total_equity"], st["position_count"], st["scan_interval"])
		}
	}

	st := get("/api/strategies/00e95f8a-baf4-4d80-85fb-9ce5060e7fbb")
	cfg, _ := st["config"].(map[string]any)
	copyCfg, _ := cfg["copy_config"].(map[string]any)
	fmt.Printf("\n=== HL Copy 0x6859 strategy ===\n")
	fmt.Printf("dry_run=%v leader=%v size_mode=%v notional=%v max_lev=%v\n",
		copyCfg["dry_run"], copyCfg["leader_address"], copyCfg["size_mode"], copyCfg["notional_usd"], copyCfg["max_leverage"])
}
