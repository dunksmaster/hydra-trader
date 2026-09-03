package hyperliquid

import (
	"strings"
	"time"

	"github.com/sonirico/go-hyperliquid"
)

// IsTransientAPIError reports Hyperliquid info/rate-limit failures that clear on retry.
func IsTransientAPIError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "api error 0") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "temporarily unavailable") ||
		strings.Contains(msg, "failed to fetch user state") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "eof")
}

func userStateWithRetry(t *HyperliquidTrader) (*hyperliquid.UserState, error) {
	var last error
	delays := []time.Duration{0, 400 * time.Millisecond, 900 * time.Millisecond, 1800 * time.Millisecond}
	for i, d := range delays {
		if d > 0 {
			time.Sleep(d)
		}
		state, err := t.exchange.Info().UserState(t.ctx, t.walletAddr)
		if err == nil {
			return state, nil
		}
		last = err
		if !IsTransientAPIError(err) || i == len(delays)-1 {
			break
		}
	}
	return nil, last
}
