//go:build ignore

// Rollout helper: stop Autopilot, close HYPE short, start Autopilot Copy (dry-run).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"nofx/auth"
)

const (
	userID          = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	autopilotTrader = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502"
	copyTrader      = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468"
)

func main() {
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
	token, err := auth.GenerateJWT(userID, "copy-rollout@local")
	if err != nil {
		panic(err)
	}
	client := &http.Client{}
	do := func(method, path string, payload any) ([]byte, int) {
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, baseURL+path, body)
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return out, resp.StatusCode
	}

	fmt.Println("Stopping NOFX Autopilot...")
	body, status := do(http.MethodPost, "/api/traders/"+autopilotTrader+"/stop", nil)
	fmt.Printf("  stop status=%d body=%s\n", status, string(body))

	fmt.Println("Closing HYPE short on Autopilot (if open)...")
	body, status = do(http.MethodPost, "/api/traders/"+autopilotTrader+"/close-position", map[string]string{
		"symbol": "HYPEUSDT",
		"side":   "SHORT",
	})
	fmt.Printf("  close status=%d body=%s\n", status, string(body))

	fmt.Println("Starting Autopilot Copy (dry-run)...")
	body, status = do(http.MethodPost, "/api/traders/"+copyTrader+"/start", nil)
	fmt.Printf("  start status=%d body=%s\n", status, string(body))
	if status >= 300 {
		os.Exit(1)
	}

	fmt.Println("Fetching copy trader status...")
	body, status = do(http.MethodGet, "/api/status?trader_id="+copyTrader, nil)
	fmt.Printf("  status=%d body=%s\n", status, string(body))
}
