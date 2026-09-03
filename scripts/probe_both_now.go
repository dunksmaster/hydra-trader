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
	bigg := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	copyID := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468"

	get := func(path string) map[string]any {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Authorization", "Bearer "+t)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return out
	}

	for _, id := range []string{bigg, copyID} {
		cfg := get("/api/traders/" + id + "/config")
		acct := get("/api/account?trader_id=" + id)
		posRaw, _ := http.NewRequest("GET", base+"/api/positions?trader_id="+id, nil)
		posRaw.Header.Set("Authorization", "Bearer "+t)
		resp, _ := http.DefaultClient.Do(posRaw)
		posB, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("\n=== %v ===\n", cfg["trader_name"])
		fmt.Printf("running=%v scan=%v equity=%v avail=%v positions=%v margin=%v%%\n",
			cfg["is_running"], cfg["scan_interval_minutes"],
			acct["total_equity"], acct["available_balance"], acct["position_count"], acct["margin_used_pct"])
		fmt.Printf("positions_json=%s\n", string(posB))
	}

	cfg := get("/api/traders/" + bigg + "/config")
	sid := fmt.Sprint(cfg["strategy_id"])
	st := get("/api/strategies/" + sid)
	c, _ := st["config"].(map[string]any)
	ai, _ := c["ai_config"].(map[string]any)
	cs, _ := ai["coin_source"].(map[string]any)
	fmt.Printf("\nBigG strategy %s: category=%v direction=%v\n", sid, cs["hyper_rank_category"], cs["hyper_rank_direction"])

	req, _ := http.NewRequest("GET", base+"/api/decisions?trader_id="+bigg+"&limit=3", nil)
	req.Header.Set("Authorization", "Bearer "+t)
	resp, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var decs []map[string]any
	_ = json.Unmarshal(b, &decs)
	for i, d := range decs {
		fmt.Printf("\n--- BigG cycle[%d] success=%v ---\n", i, d["success"])
		if log, ok := d["execution_log"].([]any); ok {
			for _, l := range log {
				fmt.Println(" ", l)
			}
		}
	}
}
