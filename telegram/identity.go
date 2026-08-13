package telegram

import (
	"fmt"
	"nofx/store"
	"os"
	"strings"
)

// resolveBotUserID returns the dashboard user ID for Telegram API calls.
// Prefers the configured owner, then any user who owns traders, then the
// oldest account — so stray test registrations cannot hijack the bot.
func resolveBotUserID(st *store.Store) (string, error) {
	if ownerID := strings.TrimSpace(os.Getenv("TELEGRAM_OWNER_USER_ID")); ownerID != "" {
		if user, err := st.User().GetByID(ownerID); err == nil && user.ID != "" {
			return user.ID, nil
		}
		return ownerID, nil
	}

	traders, err := st.Trader().ListAll()
	if err == nil && len(traders) > 0 {
		counts := make(map[string]int)
		for _, tr := range traders {
			if tr.UserID != "" {
				counts[tr.UserID]++
			}
		}
		bestID := ""
		bestCount := 0
		for id, n := range counts {
			if n > bestCount {
				bestID = id
				bestCount = n
			}
		}
		if bestID != "" {
			return bestID, nil
		}
	}

	users, err := st.User().GetAll()
	if err == nil && len(users) > 0 {
		return users[0].ID, nil
	}

	return "", fmt.Errorf("no user found")
}
