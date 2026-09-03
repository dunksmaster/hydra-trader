package telegram

import "strings"

// copyTraderDisplayName adds friendly aliases shown in Telegram lists.
func copyTraderDisplayName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "machibigbrother":
		return "machibigbrother (mochi)"
	default:
		return strings.TrimSpace(name)
	}
}

// copyTraderAliasTokens returns extra search tokens for /fav and name matching.
func copyTraderAliasTokens(name string) []string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "machibigbrother":
		return []string{"mochi", "machi", "machibig", "machibigbrother"}
	default:
		return nil
	}
}

func tokenMatchesCopyTraderName(lowerName, tok string) bool {
	if lowerName == tok || strings.Contains(lowerName, tok) {
		return true
	}
	for _, alias := range copyTraderAliasTokens(lowerName) {
		if alias == tok || strings.Contains(alias, tok) {
			return true
		}
	}
	return false
}
