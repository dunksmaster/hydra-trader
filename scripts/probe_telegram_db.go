//go:build ignore

package main

import (
	"fmt"
	"os"

	"nofx/store"
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/app/data/data.db"
	}
	if _, err := os.Stat(dbPath); err != nil {
		dbPath = "data/data.db"
	}

	st, err := store.New(dbPath)
	if err != nil {
		panic(err)
	}
	cfg, err := st.TelegramConfig().Get()
	if err != nil {
		panic(err)
	}

	fmt.Printf("bot_configured=%v\n", cfg.BotToken.String() != "")
	fmt.Printf("bound_chat_id=%d\n", cfg.ChatID)
	fmt.Printf("username=%s\n", cfg.Username)
	fmt.Printf("notify_enabled=%v\n", st.TelegramConfig().IsNotifyEnabled())
	fmt.Printf("digest_enabled=%v\n", st.TelegramConfig().IsDigestEnabled())
	fmt.Printf("pnl_swing_threshold=%.2f\n", st.TelegramConfig().GetPnlSwingThreshold())
	fmt.Printf("selected_trader=%q\n", st.TelegramConfig().GetSelectedTraderID())
	fmt.Printf("language=%s\n", st.TelegramConfig().GetLanguage())
	fmt.Printf("last_daily_digest=%s\n", st.TelegramConfig().GetLastDailyDigestDate())
}
