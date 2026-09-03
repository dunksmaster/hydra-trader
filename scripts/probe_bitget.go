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
	userID := os.Getenv("TELEGRAM_OWNER_USER_ID")
	if userID == "" {
		userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	}
	traderID := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing")
		os.Exit(1)
	}
	auth.SetJWTSecret(secret)

	token, err := auth.GenerateJWT(userID, "deploy-handoff@local")
	if err != nil {
		fmt.Fprintf(os.Stderr, "jwt: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{}
	get := func(path string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "http %s: %v\n", path, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		if out == nil {
			out = map[string]any{"raw": string(body)}
		}
		return resp.StatusCode, out
	}

	accountStatus, account := get(fmt.Sprintf("/api/exchange-account-status?trader_id=%s", traderID))
	accountInfo, acct := get(fmt.Sprintf("/api/account?trader_id=%s", traderID))
	positionsStatus, positions := get(fmt.Sprintf("/api/positions?trader_id=%s", traderID))

	fmt.Printf("account_status_http=%d\n", accountStatus)
	if errMsg, _ := account["error"].(string); errMsg != "" {
		fmt.Printf("account_status_error=%s\n", errMsg)
	}
	if status, _ := account["status"].(string); status != "" {
		fmt.Printf("exchange_status=%s\n", status)
	}
	fmt.Printf("account_http=%d equity=%v available=%v\n", accountInfo, acct["total_equity"], acct["available_balance"])
	fmt.Printf("positions_http=%d count=%d\n", positionsStatus, lenPositions(positions))
}

func lenPositions(v map[string]any) int {
	switch t := v["positions"].(type) {
	case []any:
		return len(t)
	default:
		return 0
	}
}
