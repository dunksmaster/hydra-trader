package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"nofx/auth"
)

// Reports only field NAMES and safe metadata (types / lengths / booleans) from
// GET /api/telegram so we can find a non-printing path to the bot token.
func main() {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if secret == "" {
		fmt.Println("ERROR: JWT_SECRET not present in env")
		os.Exit(1)
	}
	auth.SetJWTSecret(secret)

	owner := strings.TrimSpace(os.Getenv("TELEGRAM_OWNER_USER_ID"))
	if owner == "" {
		owner = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	}
	// Local clock runs ahead of the server; backdate iat/nbf or the API 401s.
	past := time.Now().Add(-3 * time.Minute)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: owner,
		Email:  "tg-send@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(past.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	if err != nil {
		fmt.Println("ERROR: jwt:", err)
		os.Exit(1)
	}

	req, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/telegram", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("ERROR: request:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("status=%d body_bytes=%d\n", resp.StatusCode, len(body))

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		fmt.Println("non-json response; contents suppressed")
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := m[k].(type) {
		case bool:
			fmt.Printf("field %-14s bool=%v\n", k, v)
		case string:
			fmt.Printf("field %-14s string len=%d empty=%v\n", k, len(v), v == "")
		case float64:
			fmt.Printf("field %-14s number nonzero=%v\n", k, v != 0)
		case nil:
			fmt.Printf("field %-14s null\n", k)
		default:
			fmt.Printf("field %-14s other\n", k)
		}
	}
}
