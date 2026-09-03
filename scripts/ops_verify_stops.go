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

	"github.com/golang-jwt/jwt/v5"
)

const userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"

func main() {
	baseURL := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "verify-stop@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 180 * time.Second}

	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("GET %s err=%v\n", path, err)
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b
	}
	post := func(path string) {
		req, _ := http.NewRequest(http.MethodPost, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("POST %s err=%v\n", path, err)
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("POST %s → %d %s\n", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}

	want := map[string]bool{}
	for _, n := range []string{"Copy L4", "Grinder", "Crypto BigG", "NOFX Autopilot"} {
		want[strings.ToLower(n)] = true
	}

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		id := firstStr(tr, "trader_id", "id")
		running := fmt.Sprint(tr["is_running"]) == "true"
		low := strings.ToLower(name)
		match := want[low] || strings.Contains(low, "leviathan")
		if !match {
			fmt.Printf("KEEP  %-22s running=%v\n", name, running)
			continue
		}
		fmt.Printf("TARGET %-22s running=%v\n", name, running)
		if running {
			post("/api/traders/" + id + "/stop")
			time.Sleep(3 * time.Second)
		}
	}

	fmt.Println("\n--- recheck ---")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		running := fmt.Sprint(tr["is_running"]) == "true"
		sid := firstStr(tr, "strategy_id")
		paused := ""
		if sid != "" {
			var st map[string]any
			_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
			if c, ok := st["config"].(map[string]any); ok {
				if cc, ok := c["copy_config"].(map[string]any); ok {
					paused = fmt.Sprintf(" copy_paused=%v", cc["copy_paused"])
				}
			}
		}
		fmt.Printf("%-22s running=%v%s\n", name, running, paused)
	}
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
