package telegram

import (
	"fmt"
	"net/url"
	"nofx/auth"
	"nofx/logger"
	"nofx/store"
	"os"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func publicAppURL() string {
	if u := strings.TrimSpace(os.Getenv("NOFX_PUBLIC_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := strings.TrimSpace(os.Getenv("RAILWAY_PUBLIC_DOMAIN")); u != "" {
		return "https://" + strings.TrimRight(u, "/")
	}
	return "https://nofx-production-fcd1.up.railway.app"
}

func generateWebLoginToken(st *store.Store, userID string) (string, error) {
	email := "dashboard@nofx"
	if user, err := st.User().GetByID(userID); err == nil && user.Email != "" {
		email = user.Email
	}
	return auth.GenerateJWT(userID, email)
}

func handleWebLoginCommand(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, botUserID string) {
	token, err := generateWebLoginToken(st, botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not create login link. Try again in a moment.")
		return
	}
	link := fmt.Sprintf(
		"%s/auth/callback?token=%s&redirect=%s",
		publicAppURL(),
		url.QueryEscape(token),
		url.QueryEscape("/dashboard"),
	)
	// #region agent log
	logger.Infof("[DBG-e70047] hypothesis=H3 location=weblogin.go:handleWebLoginCommand message=weblogin_link_created user_id=%q", botUserID)
	// #endregion
	sendMsg(bot, chatID, "Tap to open your NOFX dashboard (valid 7 days):\n\n"+link)
}
