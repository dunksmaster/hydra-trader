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
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "wallets@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b
	}

	var exchanges []map[string]any
	_ = json.Unmarshal(get("/api/exchanges"), &exchanges)

	hlAddrs := map[string]string{}
	fmt.Printf("TRADING EXCHANGE ACCOUNTS: %d\n", len(exchanges))
	for _, e := range exchanges {
		id := fmt.Sprint(e["id"])
		idShort := id
		if len(idShort) > 8 {
			idShort = idShort[:8]
		}
		addr := strings.TrimSpace(fmt.Sprint(e["hyperliquid_wallet_addr"]))
		if addr == "" || addr == "<nil>" {
			addr = strings.TrimSpace(fmt.Sprint(e["hyperliquidWalletAddr"]))
		}
		if addr == "" || addr == "<nil>" {
			addr = "-"
		} else {
			hlAddrs[strings.ToLower(addr)] = fmt.Sprint(e["account_name"])
		}
		fmt.Printf("  • %s [%s] type=%s enabled=%v hl=%s\n",
			e["account_name"], idShort, e["exchange_type"], e["enabled"], maskAddr(addr))
	}

	var models []map[string]any
	_ = json.Unmarshal(get("/api/models"), &models)
	clawModels := 0
	for _, m := range models {
		if fmt.Sprint(m["provider"]) == "claw402" {
			clawModels++
		}
	}
	fmt.Printf("\nCLAW402 AI-PAY MODELS (Base USDC billing): %d\n", clawModels)

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	running := 0
	for _, tr := range traders {
		if fmt.Sprint(tr["is_running"]) == "true" {
			running++
		}
	}
	fmt.Printf("\nBOTS: %d total, %d running\n", len(traders), running)
	fmt.Printf("UNIQUE HYPERLIQUID ON-CHAIN ADDRESSES (exchange config): %d\n", len(hlAddrs))
}

func maskAddr(addr string) string {
	if addr == "-" || !strings.HasPrefix(addr, "0x") || len(addr) < 12 {
		return addr
	}
	return addr[:6] + "…" + addr[len(addr)-4:]
}
