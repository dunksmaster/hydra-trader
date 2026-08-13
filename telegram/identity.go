package telegram

import (
	"fmt"
	"nofx/store"
	"os"
	"strings"
)

// resolveBotUserID returns the dashboard user ID for Telegram API calls.
// Falls back to trader owner or TELEGRAM_OWNER_USER_ID when users table is empty.
func resolveBotUserID(st *store.Store) (string, error) {
	users, err := st.User().GetAll()
	if err == nil && len(users) > 0 {
		return users[0].ID, nil
	}
	traders, err := st.Trader().ListAll()
	if err == nil && len(traders) > 0 && traders[0].UserID != "" {
		return traders[0].UserID, nil
	}
	if id := strings.TrimSpace(os.Getenv("TELEGRAM_OWNER_USER_ID")); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("no user found")
}
