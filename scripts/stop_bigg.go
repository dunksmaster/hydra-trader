//go:build ignore

package main

import (
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

	token, err := auth.GenerateJWT(userID, "deploy-preflight@local")
	if err != nil {
		fmt.Fprintf(os.Stderr, "jwt: %v\n", err)
		os.Exit(1)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/traders/"+traderID+"/stop", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "http: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("status=%d body=%s\n", resp.StatusCode, string(body))
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}
}
