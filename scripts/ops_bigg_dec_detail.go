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

const (
	userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
)

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "bigg-dec@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, _ := client.Do(req)
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}

	raw := get("/api/decisions?trader_id=" + biggID + "&limit=30")
	var decs []map[string]any
	_ = json.Unmarshal(raw, &decs)
	cutoff := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	fmt.Printf("decisions=%d\n", len(decs))
	for _, d := range decs {
		ts := parseTime(d["timestamp"])
		if ts.Before(cutoff) {
			continue
		}
		pretty, _ := json.MarshalIndent(d, "", "  ")
		s := string(pretty)
		if len(s) > 2500 {
			s = s[:2500] + "…"
		}
		fmt.Printf("\n===== %s =====\n%s\n", ts.UTC().Format(time.RFC3339), s)
	}

	fmt.Println("\n=== HL e282/lucy recent BTC/VIRTUAL ===")
	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(fmt.Sprint(tr["trader_name"]))
		if !strings.Contains(name, "e282") && name != "lucy30" {
			continue
		}
		id := fmt.Sprint(tr["trader_id"])
		fmt.Printf("\n## %s\n", tr["trader_name"])
		var orders []map[string]any
		_ = json.Unmarshal(get("/api/orders?trader_id="+id+"&limit=20"), &orders)
		for _, o := range orders {
			sym := strings.ToUpper(fmt.Sprint(o["symbol"]))
			if !strings.Contains(sym, "BTC") && !strings.Contains(sym, "VIRTUAL") {
				continue
			}
			ms := toMs(o["filled_at"])
			fmt.Printf("  %s %s %s qty=%v reason=%s\n",
				time.UnixMilli(ms).UTC().Format("15:04:05"),
				o["symbol"], o["order_action"], o["quantity"], trunc(fmt.Sprint(o["reasoning"]), 120))
		}
	}
}

func parseTime(v any) time.Time {
	s := fmt.Sprint(v)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func toMs(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	default:
		var f float64
		fmt.Sscan(fmt.Sprint(v), &f)
		return int64(f)
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
