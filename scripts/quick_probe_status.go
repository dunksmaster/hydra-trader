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
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "q@local")
	c := &http.Client{Timeout: 45 * time.Second}
	base := "https://nofx-production-fcd1.up.railway.app"
	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+t)
		resp, err := c.Do(req)
		if err != nil {
			fmt.Println("ERR", path, err)
			return nil, 0
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b, resp.StatusCode
	}
	body, st := get("/api/my-traders")
	fmt.Println("my-traders status", st)
	var trList []map[string]any
	_ = json.Unmarshal(body, &trList)
	for _, tr := range trList {
		name := fmt.Sprint(tr["trader_name"], tr["name"])
		if !strings.Contains(strings.ToLower(name), "copy") && name != "🐉 Leviathan" {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		sid := fmt.Sprint(tr["strategy_id"])
		leader := "?"
		if sid != "" && sid != "<nil>" {
			sb, _ := get("/api/strategies/" + sid)
			var sm map[string]any
			_ = json.Unmarshal(sb, &sm)
			if cfg, _ := sm["config"].(map[string]any); cfg != nil {
				if cc, _ := cfg["copy_config"].(map[string]any); cc != nil {
					leader = fmt.Sprint(cc["leader_address"])
					fmt.Printf("copy_on_start=%v wallet_copy_slots=%v\n", cc["copy_on_start"], cc["wallet_copy_slots"])
				}
			}
		}
		fmt.Printf("%s running=%v leader=%s id=%s\n", name, tr["is_running"], leader, id[:min(28, len(id))])
	}
	for _, tr := range trList {
		id := firstStr(tr, "trader_id", "id")
		if id == "" || id == "<nil>" {
			continue
		}
		pb, ps := get("/api/positions?trader_id=" + id)
		if ps >= 300 {
			continue
		}
		var pos []map[string]any
		_ = json.Unmarshal(pb, &pos)
		if len(pos) > 0 {
			fmt.Printf("positions on %s: %d\n", fmt.Sprint(tr["trader_name"]), len(pos))
			for _, p := range pos {
				fmt.Printf("  %v %v\n", p["symbol"], p["side"])
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "" && fmt.Sprint(v) != "<nil>" {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}
