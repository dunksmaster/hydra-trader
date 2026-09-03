package events

import "strings"

// IsTransientCopyFailureText reports Hyperliquid state/rate-limit noise that must
// never produce a copy_failed Telegram alert (emit path or notifier layer).
func IsTransientCopyFailureText(text string) bool {
	if text == "" {
		return false
	}
	msg := strings.ToLower(text)
	return strings.Contains(msg, "api error 0") ||
		strings.Contains(msg, "failed to get positions") ||
		strings.Contains(msg, "failed to fetch user state") ||
		strings.Contains(msg, "failed to get account") ||
		strings.Contains(msg, "leader now long on") ||
		strings.Contains(msg, "leader now short on") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "temporarily unavailable") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, " eof")
}
