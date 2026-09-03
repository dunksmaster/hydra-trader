//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"nofx/auth"
	hlprovider "nofx/provider/hyperliquid"
)

const leviathanID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787218997"
const leader = "0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf"

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "diag@local")
	c := &http.Client{Timeout: 45 * time.Second}
	get := func(path string) string {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+t)
		resp, err := c.Do(req)
		if err != nil {
			return "ERR: " + err.Error()
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return string(b)
	}

	fmt.Println("=== my-traders ===")
	var trs []map[string]any
	_ = json.Unmarshal([]byte(get("/api/my-traders")), &trs)
	for _, tr := range trs {
		name := fmt.Sprint(tr["trader_name"])
		id := fmt.Sprint(tr["trader_id"])
		fmt.Printf("%s running=%v id=%s\n", name, tr["is_running"], id[:min(40, len(id))])
		if strings.Contains(name, "Leviathan") || id == leviathanID {
			sid := fmt.Sprint(tr["strategy_id"])
			var st map[string]any
			_ = json.Unmarshal([]byte(get("/api/strategies/"+sid)), &st)
			cfg, _ := st["config"].(map[string]any)
			cc, _ := cfg["copy_config"].(map[string]any)
			fmt.Printf("  copy_mode=%v copy_on_start=%v leader=%v max_pos=%v dry_run=%v\n",
				cc["copy_mode"], cc["copy_on_start"], cc["leader_address"], cc["max_positions"], cc["dry_run"])
		}
	}

	fmt.Println("\n=== Leviathan account ===")
	fmt.Println(get("/api/account?trader_id=" + leviathanID))
	fmt.Println("\n=== Leviathan positions ===")
	fmt.Println(get("/api/positions?trader_id=" + leviathanID))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	since := time.Now().UTC().Add(-2 * time.Hour).UnixMilli()
	var fills []map[string]any
	_ = hlprovider.PostInfo(ctx, map[string]any{"type": "userFills", "user": leader}, &fills)
	fmt.Printf("\n=== Leader fills last 2h (sample %d) ===\n", len(fills))
	nOpen := 0
	for _, f := range fills {
		ts, _ := f["time"].(float64)
		if int64(ts) < since {
			continue
		}
		dir := fmt.Sprint(f["dir"])
		coin := fmt.Sprint(f["coin"])
		if strings.Contains(dir, "Open") {
			nOpen++
			fmt.Printf("  %s %s @ %s\n", time.UnixMilli(int64(ts)).UTC().Format("15:04:05"), coin, dir)
		}
	}
	if nOpen == 0 {
		fmt.Println("  (no new Open fills in last 2 hours — bot only copies NEW opens)")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
