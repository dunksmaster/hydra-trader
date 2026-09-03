//go:build ignore

package main

import ("encoding/json"; "fmt"; "io"; "net/http"; "os"; "strings"; "time"; "nofx/auth")
func main() {
  auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
  t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "verify@local")
  base := "https://nofx-production-fcd1.up.railway.app"
  traderID := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
  get := func(path string) []byte {
    req, _ := http.NewRequest("GET", base+path, nil)
    req.Header.Set("Authorization", "Bearer "+t)
    resp, err := http.DefaultClient.Do(req)
    if err != nil { panic(err) }
    defer resp.Body.Close()
    b, _ := io.ReadAll(resp.Body)
    fmt.Printf("GET %s status=%d\n", path, resp.StatusCode)
    return b
  }
  var cfg map[string]any
  json.Unmarshal(get("/api/traders/"+traderID+"/config"), &cfg)
  fmt.Printf("trader=%v running=%v scan_min=%v strategy=%v\n", cfg["trader_name"], cfg["is_running"], cfg["scan_interval_minutes"], cfg["strategy_id"])
  var latest []any
  json.Unmarshal(get("/api/decisions/latest?trader_id="+traderID), &latest)
  fmt.Printf("latest_decisions_count=%d\n", len(latest))
  for i, raw := range latest {
    if i >= 2 { break }
    d, _ := raw.(map[string]any)
    ts, _ := d["timestamp"].(string)
    dur, _ := d["ai_call_duration_sec"].(float64)
    reasoning, _ := d["reasoning"].(string)
    if len(reasoning) > 400 { reasoning = reasoning[:400] + "..." }
    fmt.Printf("--- decision[%d] ts=%s ai_duration=%.2fs ---\n%s\n", i, ts, dur, reasoning)
    if decs, ok := d["decisions"].([]any); ok {
      for j, dr := range decs {
        if j >= 5 { break }
        dm, _ := dr.(map[string]any)
        fmt.Printf("  [%d] %v %v\n", j, dm["symbol"], dm["action"])
      }
    }
  }
  fmt.Printf("now_utc=%s\n", time.Now().UTC().Format(time.RFC3339))
}
