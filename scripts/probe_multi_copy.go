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
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "multi-copy-verify@local")
	client := &http.Client{}

	getJSON := func(path string) json.RawMessage {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return body
	}

	getMap := func(path string) map[string]any {
		var out map[string]any
		_ = json.Unmarshal(getJSON(path), &out)
		return out
	}

	var trList []map[string]any
	raw := getJSON("/api/my-traders")
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil {
		trList = arr
	} else {
		var wrap map[string]any
		_ = json.Unmarshal(raw, &wrap)
		if items, ok := wrap["traders"].([]any); ok {
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					trList = append(trList, m)
				}
			}
		}
	}

	fmt.Printf("=== Copy bots ===\n")
	n := 0
	for _, tr := range trList {
		name := fmt.Sprint(tr["name"], tr["trader_name"])
		if !strings.Contains(strings.ToLower(name), "copy") {
			continue
		}
		n++
		id := firstStr(tr, "trader_id", "id")
		running := fmt.Sprint(tr["is_running"]) == "true"
		sid := fmt.Sprint(tr["strategy_id"])
		leader := "?"
		maxPos := "?"
		notional := "?"
		if sid != "" && sid != "<nil>" {
			st := getMap("/api/strategies/" + sid)
			cfg, _ := st["config"].(map[string]any)
			copyCfg, _ := cfg["copy_config"].(map[string]any)
			leader = fmt.Sprint(copyCfg["leader_address"])
			maxPos = fmt.Sprint(copyCfg["max_positions"])
			notional = fmt.Sprint(copyCfg["notional_usd"])
		}
		shortID := id
		if len(shortID) > 24 {
			shortID = shortID[:24] + "..."
		}
		fmt.Printf("%d) %s running=%v leader=%s notional=$%s max_pos=%s id=%s\n",
			n, name, running, shortAddr(leader), notional, maxPos, shortID)
	}
	acct := getMap("/api/account?trader_id=8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468")
	fmt.Printf("\nWallet equity=$%v available=$%v positions=%v\n",
		acct["total_equity"], acct["available_balance"], acct["position_count"])
	fmt.Printf("Total copy bots found: %d\n", n)
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "" {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func shortAddr(a string) string {
	if len(a) <= 12 {
		return a
	}
	return a[:6] + "..." + a[len(a)-4:]
}
