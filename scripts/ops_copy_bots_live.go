//go:build ignore

// Compare /api/copy-bots in-memory running vs /api/my-traders DB flags.
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
		UserID: userID, Email: "copy-bots-live@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}
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

	db := map[string]bool{}
	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		id := firstStr(tr, "trader_id", "id")
		db[id] = fmt.Sprint(tr["is_running"]) == "true"
		_ = name
	}

	var payload map[string]any
	_ = json.Unmarshal(get("/api/copy-bots"), &payload)
	bots, _ := payload["bots"].([]any)
	if bots == nil {
		// flat array fallback
		var arr []map[string]any
		_ = json.Unmarshal(get("/api/copy-bots"), &arr)
		for _, b := range arr {
			bots = append(bots, b)
		}
	}

	fmt.Println("=== copy-bots in-memory vs my-traders DB ===")
	memRunning := 0
	for _, raw := range bots {
		b, _ := raw.(map[string]any)
		if b == nil {
			continue
		}
		id := firstStr(b, "trader_id")
		name := firstStr(b, "trader_name")
		mem := fmt.Sprint(b["is_running"]) == "true"
		paused := ""
		if cc, ok := b["copy_config"].(map[string]any); ok && cc != nil {
			paused = fmt.Sprintf(" paused=%v", cc["copy_paused"])
		}
		dbr := db[id]
		flag := "ok"
		if mem != dbr {
			flag = "MISMATCH"
		}
		if mem {
			memRunning++
		}
		fmt.Printf("%-22s mem=%-5v db=%-5v %s%s\n", name, mem, dbr, flag, paused)
	}
	fmt.Printf("\nIn-memory running count: %d\n", memRunning)
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
