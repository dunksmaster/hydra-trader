//go:build ignore

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"nofx/auth"

	"github.com/golang-jwt/jwt/v5"
)

const biggID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: "08ab3fcb-8486-45cf-bd27-0ad35443ff61", Email: "stop-bigg@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	req, _ := http.NewRequest(http.MethodPost, "https://nofx-production-fcd1.up.railway.app/api/traders/"+biggID+"/stop", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stop failed: %v\n", err)
		os.Exit(1)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("stop status=%d body=%s\n", resp.StatusCode, string(b))
}
