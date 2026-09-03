//go:build ignore

// One-shot checklist: fetch Railway server IP for Bitget API whitelist and print verification steps.
// Run: go run scripts/ops_bitget_key_checklist.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("NOFX_BASE_URL")), "/")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	token := strings.TrimSpace(os.Getenv("NOFX_JWT"))
	if token == "" {
		fmt.Println("Set NOFX_JWT to a valid session token, then re-run.")
		fmt.Println("Or open Settings → Server IP in the NOFX dashboard after login.")
		os.Exit(1)
	}

	req, err := http.NewRequest(http.MethodGet, base+"/api/server-ip", nil)
	if err != nil {
		fmt.Println("request error:", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("fetch error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("server-ip HTTP %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var out struct {
		IP      string `json:"ip"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &out)

	fmt.Println("=== Bitget API key security checklist (Crypto BigG) ===")
	fmt.Println()
	if out.IP != "" {
		fmt.Printf("Railway egress IP to whitelist on Bitget: %s\n", out.IP)
	} else {
		fmt.Println("Could not parse server IP from response:", string(body))
	}
	fmt.Println()
	fmt.Println("On Bitget → API Management → your BigG key, confirm:")
	fmt.Println("  [ ] Withdrawals DISABLED")
	fmt.Println("  [ ] Permissions: Trade + Read only (no transfer/withdraw)")
	fmt.Println("  [ ] IP whitelist includes the Railway egress IP above")
	fmt.Println()
	fmt.Println("Hyperliquid agent keys cannot withdraw by protocol; Bitget is the only total-loss vector.")
}
