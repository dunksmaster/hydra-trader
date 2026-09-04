//go:build ignore

// Close machibigbrother HYPE short, re-apply watch-only, restart fills loop.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"nofx/auth"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: "08ab3fcb-8486-45cf-bd27-0ad35443ff61",
		Email:  "machi-close@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	postJSON := func(path string, body any) (int, string) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}

	var traders []map[string]any
	req, _ := http.NewRequest(http.MethodGet, base+"/api/my-traders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	_ = json.Unmarshal(b, &traders)

	var id string
	for _, t := range traders {
		if strings.Contains(strings.ToLower(fmt.Sprint(t["trader_name"])), "machibig") {
			id = fmt.Sprint(t["trader_id"])
			break
		}
	}
	fmt.Println("machi id:", id)

	sc, sb := postJSON("/api/traders/"+id+"/close-position", map[string]string{
		"symbol": "HYPEUSDT",
		"side":   "SHORT",
	})
	fmt.Println("close HYPE SHORT:", sc, sb)

	sc, sb = post("/api/traders/" + id + "/stop")
	fmt.Println("stop:", sc, sb)
	time.Sleep(3 * time.Second)
	sc, sb = post("/api/traders/" + id + "/start")
	fmt.Println("start:", sc, sb)
}
