package telegram

import (
	"nofx/api"
	"nofx/config"
	"nofx/logger"
	"nofx/mcp"
	_ "nofx/mcp/payment"
	_ "nofx/mcp/provider"
	"nofx/store"
	"nofx/telegram/agent"
	"os"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Start initializes and runs the Telegram bot in a blocking supervisor loop.
// Supports hot-reload: when a signal is sent on reloadCh, the bot restarts
// with the latest token (re-read from DB or env). Must be called as a goroutine from main.go.
func Start(cfg *config.Config, st *store.Store, reloadCh <-chan struct{}) {
	for {
		token := resolveToken(cfg, st)
		if token == "" {
			logger.Info("Telegram bot disabled (no token configured), waiting for reload signal...")
			<-reloadCh
			continue
		}

		stopped := runBot(token, cfg, st, reloadCh)
		if !stopped {
			return
		}
		logger.Info("Reloading Telegram bot with latest token...")
	}
}

// resolveToken returns the bot token from DB (configured via Web UI), then TELEGRAM_BOT_TOKEN env.
func resolveToken(cfg *config.Config, st *store.Store) string {
	dbCfg, err := st.TelegramConfig().Get()
	if err == nil && dbCfg.BotToken != "" {
		return dbCfg.BotToken
	}
	return strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
}

// runBot runs the bot until a reload is requested, the updates channel closes, or a fatal error.
func runBot(token string, cfg *config.Config, st *store.Store, reloadCh <-chan struct{}) bool {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		logger.Errorf("Telegram bot failed to start: %v", err)
		<-reloadCh
		return true
	}
	logger.Infof("Telegram bot @%s started", bot.Self.UserName)

	registerBotCommands(bot)
	InitNotifier(st, bot, cfg.APIServerPort)

	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-reloadCh:
			logger.Info("Reload signal received, stopping current Telegram bot...")
			bot.StopReceivingUpdates()
		case <-stopWatch:
		}
	}()
	defer close(stopWatch)

	// Allowed chat ID: read from DB binding (0 = unbound; /start requires dashboard bind code).
	allowedChatID := int64(0)
	if id, err := st.TelegramConfig().GetBoundChatID(); err == nil && id != 0 {
		allowedChatID = id
	} else if _, err := st.TelegramConfig().EnsureBindCode(); err != nil {
		logger.Warnf("Telegram: failed to ensure bind code: %v", err)
	}

	// botUserID / botToken / agents are resolved lazily and refresh when user registers.
	var (
		botUserID    string
		botUserEmail string
		botToken     string
		agents       *agent.Manager
	)

	resolveBotUser := func() bool {
		userID, err := resolveBotUserID(st)
		if err != nil {
			return false
		}
		if userID == botUserID {
			return true
		}
		users, _ := st.User().GetAll()
		email := "dashboard"
		if len(users) > 0 && users[0].ID == userID {
			email = users[0].Email
		}
		newToken, err := agent.GenerateBotToken(userID)
		if err != nil {
			logger.Errorf("Failed to generate bot JWT for user %s: %v", userID, err)
			return false
		}
		prev := botUserID
		botUserID = userID
		botUserEmail = email
		botToken = newToken
		agents = agent.NewManager(cfg.APIServerPort, botToken, botUserEmail, botUserID,
			func() mcp.AIClient { return newLLMClient(st, botUserID) },
			api.GetAPIDocs(),
		)
		if prev == "" {
			logger.Infof("Bot: resolved user %s (%s)", botUserID, botUserEmail)
		} else {
			logger.Infof("Bot: user changed → %s (%s)", botUserID, botUserEmail)
		}
		return true
	}
	resolveBotUser()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// awaitingLang is set only when the user explicitly runs /lang.
	awaitingLang := false

	for update := range updates {
		// ── Inline trader picker ────────────────────────────────────────────
		if update.CallbackQuery != nil {
			if allowedChatID != 0 && update.CallbackQuery.Message != nil &&
				update.CallbackQuery.Message.Chat.ID != allowedChatID {
				cb := tgbotapi.NewCallback(update.CallbackQuery.ID, "Unauthorized")
				bot.Request(cb) //nolint:errcheck
				continue
			}
			if strings.HasPrefix(update.CallbackQuery.Data, "use:") {
				resolveBotUser()
				lang := st.TelegramConfig().GetLanguage()
				handleTraderUseCallback(bot, update.CallbackQuery, st, lang, botUserID, cfg.APIServerPort)
			}
			continue
		}

		if update.Message == nil {
			continue
		}
		chatID := update.Message.Chat.ID
		text := strings.TrimSpace(update.Message.Text)

		// ── Language selection (triggered only by /lang) ──────────────────────
		if awaitingLang && chatID == allowedChatID {
			if lang := parseLangChoice(text); lang != "" {
				awaitingLang = false
				st.TelegramConfig().SetLanguage(lang) //nolint:errcheck
				sendStatusWithKeyboard(bot, chatID, st, botUserID, cfg.APIServerPort, lang)
			} else {
				sendMarkdownMsg(bot, chatID, langMenuMsg())
			}
			continue
		}

		// ── /start ────────────────────────────────────────────────────────────
		if isStartCommand(text) {
			resolveBotUser()
			if botUserID == "" {
				sendMsg(bot, chatID,
					"No account found.\nOpen the web dashboard to register, then send /start.")
				continue
			}
			if allowedChatID == 0 {
				bindCode := parseStartBindCode(text)
				if bindCode == "" {
					sendMsg(bot, chatID,
						"Binding requires a code from the NOFX dashboard.\nOpen Settings → Telegram and send:\n/start YOUR_CODE")
					continue
				}
				username := update.Message.From.UserName
				if err := st.TelegramConfig().BindUser(chatID, "@"+username, bindCode); err != nil {
					logger.Errorf("Failed to bind Telegram user: %v", err)
					sendMsg(bot, chatID, "Invalid or expired bind code. Copy a fresh code from the NOFX dashboard.")
					continue
				}
				allowedChatID = chatID
				logger.Infof("Telegram bound to @%s (chatID: %d)", username, chatID)
			} else if chatID != allowedChatID {
				sendMsg(bot, chatID, "This bot is already bound to another account.")
				continue
			} else {
				agents.Reset(chatID)
			}
			lang := st.TelegramConfig().GetLanguage()
			sendStatusWithKeyboard(bot, chatID, st, botUserID, cfg.APIServerPort, lang)
			continue
		}

		// ── /lang ─────────────────────────────────────────────────────────────
		if text == "/lang" {
			awaitingLang = true
			sendMarkdownMsg(bot, chatID, langMenuMsg())
			continue
		}

		// ── /help ─────────────────────────────────────────────────────────────
		if text == "/help" {
			lang := st.TelegramConfig().GetLanguage()
			sendMarkdownMsg(bot, chatID, helpMsg(lang))
			continue
		}

		// ── Quick commands (no AI) ───────────────────────────────────────────
		if isQuickCommand(text) {
			if allowedChatID != 0 && chatID != allowedChatID {
				sendMsg(bot, chatID, "Unauthorized.")
				continue
			}
			if allowedChatID == 0 {
				sendMsg(bot, chatID, "Send /start first.")
				continue
			}
			resolveBotUser()
			if botUserID == "" {
				sendMsg(bot, chatID, "No account found. Open the web dashboard to register.")
				continue
			}
			handleQuickCommand(bot, chatID, text, st, botUserID, cfg.APIServerPort)
			continue
		}

		// ── Access control ────────────────────────────────────────────────────
		if allowedChatID != 0 && chatID != allowedChatID {
			sendMsg(bot, chatID, "Unauthorized.")
			continue
		}
		if allowedChatID == 0 {
			sendMsg(bot, chatID, "Send /start first.")
			continue
		}
		if text == "" {
			continue
		}

		// ── Plain-text shortcuts (same as /positions, /balanca, etc.) ────────
		if intent := matchNLQuickIntent(text); intent != "" {
			resolveBotUser()
			if botUserID == "" {
				sendMsg(bot, chatID, "No account found. Open the web dashboard to register.")
				continue
			}
			handleQuickCommand(bot, chatID, "/"+intent, st, botUserID, cfg.APIServerPort)
			continue
		}

		// ── Refresh user before every AI call ────────────────────────────────
		resolveBotUser()
		if botUserID == "" {
			sendMsg(bot, chatID, "No account found. Open the web dashboard to register.")
			continue
		}

		lang := st.TelegramConfig().GetLanguage()

		// ── Guard: show status if not ready for trading ───────────────────────
		if newLLMClient(st, botUserID) == nil {
			sendMarkdownMsg(bot, chatID, statusMsg(st, botUserID, cfg.APIServerPort, lang))
			continue
		}

		// ── AI agent ─────────────────────────────────────────────────────────
		go func(chatID int64, text string) {
			sent, err := bot.Send(tgbotapi.NewMessage(chatID, "⏳"))
			placeholderID := 0
			if err == nil {
				placeholderID = sent.MessageID
			}

			var (
				mu       sync.Mutex
				lastEdit time.Time
			)
			onChunk := func(accumulated string) {
				if placeholderID == 0 {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if accumulated != "⏳" && time.Since(lastEdit) < time.Second {
					return
				}
				lastEdit = time.Now()
				edit := tgbotapi.NewEditMessageText(chatID, placeholderID, accumulated)
				bot.Send(edit) //nolint:errcheck
			}

			reply := agents.Run(chatID, text, onChunk)

			if placeholderID != 0 {
				edit := tgbotapi.NewEditMessageText(chatID, placeholderID, reply)
				edit.ParseMode = "Markdown"
				if _, err := bot.Send(edit); err != nil {
					edit2 := tgbotapi.NewEditMessageText(chatID, placeholderID, reply)
					bot.Send(edit2) //nolint:errcheck
				}
			} else {
				msg := tgbotapi.NewMessage(chatID, reply)
				msg.ParseMode = "Markdown"
				if _, err := bot.Send(msg); err != nil {
					msg.ParseMode = ""
					bot.Send(msg) //nolint:errcheck
				}
			}
		}(chatID, text)
	}

	return true
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func sendMsg(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	bot.Send(msg) //nolint:errcheck
}

func sendMarkdownMsg(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if _, err := bot.Send(msg); err != nil {
		plain := tgbotapi.NewMessage(chatID, text)
		bot.Send(plain) //nolint:errcheck
	}
}

func queryKeyboard(lang string) tgbotapi.ReplyKeyboardMarkup {
	if lang == "zh" {
		return tgbotapi.ReplyKeyboardMarkup{
			Keyboard: [][]tgbotapi.KeyboardButton{
				{{Text: "查看持仓"}, {Text: "查看余额"}},
				{{Text: "我的交易员"}},
			},
			ResizeKeyboard: true,
		}
	}
	return tgbotapi.ReplyKeyboardMarkup{
		Keyboard: [][]tgbotapi.KeyboardButton{
			{{Text: "Balanca"}, {Text: "Pozicionet"}},
			{{Text: "Tregtarët"}, {Text: "Njoftime"}},
		},
		ResizeKeyboard: true,
	}
}

func sendStatusWithKeyboard(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, userID string, apiPort int, lang string) {
	msg := tgbotapi.NewMessage(chatID, statusMsg(st, userID, apiPort, lang))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = queryKeyboard(lang)
	if _, err := bot.Send(msg); err != nil {
		plain := tgbotapi.NewMessage(chatID, statusMsg(st, userID, apiPort, lang))
		plain.ReplyMarkup = queryKeyboard(lang)
		bot.Send(plain) //nolint:errcheck
	}
}

func sendHTMLMsg(bot *tgbotapi.BotAPI, chatID int64, html string) {
	msg := tgbotapi.NewMessage(chatID, html)
	msg.ParseMode = "HTML"
	if _, err := bot.Send(msg); err != nil {
		plain := tgbotapi.NewMessage(chatID, stripHTML(html))
		bot.Send(plain) //nolint:errcheck
	}
}

func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "<b>", "")
	s = strings.ReplaceAll(s, "</b>", "")
	s = strings.ReplaceAll(s, "<i>", "")
	s = strings.ReplaceAll(s, "</i>", "")
	return s
}

func registerBotCommands(bot *tgbotapi.BotAPI) {
	cmds := []tgbotapi.BotCommand{
		{Command: "start", Description: "Bind account & status"},
		{Command: "balanca", Description: "Portfolio & balance"},
		{Command: "positions", Description: "Open positions detail"},
		{Command: "traders", Description: "Choose strategy / trader"},
		{Command: "use", Description: "Switch trader (1, 2, all)"},
		{Command: "notify", Description: "Alerts on/off/test"},
		{Command: "lang", Description: "Change language"},
		{Command: "help", Description: "All commands"},
	}
	cfg := tgbotapi.NewSetMyCommands(cmds...)
	if _, err := bot.Request(cfg); err != nil {
		logger.Warnf("Telegram: failed to set bot commands: %v", err)
	}
}

// ── LLM client ───────────────────────────────────────────────────────────────

func newLLMClient(st *store.Store, userID string) mcp.AIClient {
	// 1. Prefer the model explicitly configured for Telegram (Settings → Telegram → AI Model)
	if tgCfg, err := st.TelegramConfig().Get(); err == nil && tgCfg.ModelID != "" {
		if model, err := st.AIModel().Get(userID, tgCfg.ModelID); err == nil && model.Enabled {
			if client := buildLLMClientFromModel(userID, tgCfg.ModelID, model, "telegram_config"); client != nil {
				return client
			}
		} else {
			logger.Warnf("Telegram agent: model_id=%q not found or disabled for user=%s, falling back",
				tgCfg.ModelID, userID)
		}
	}

	// 2. Fall back to first enabled model
	if model, err := st.AIModel().GetDefault(userID); err == nil {
		if client := buildLLMClientFromModel(userID, model.ID, model, "default"); client != nil {
			return client
		}
	}

	// 3. Environment variable fallback
	for _, pair := range []struct{ provider, key, url string }{
		{"deepseek", os.Getenv("DEEPSEEK_API_KEY"), mcp.DefaultDeepSeekBaseURL},
		{"openai", os.Getenv("OPENAI_API_KEY"), ""},
		{"claude", os.Getenv("ANTHROPIC_API_KEY"), ""},
	} {
		if pair.key != "" {
			client := clientForProvider(pair.provider)
			client.SetAPIKey(pair.key, pair.url, "")
			logger.Infof("Telegram agent: user=%s source=env provider=%s", userID, pair.provider)
			return client
		}
	}
	return nil
}

func buildLLMClientFromModel(userID, modelID string, model *store.AIModel, source string) mcp.AIClient {
	if model == nil {
		return nil
	}
	apiKey := string(model.APIKey)
	if apiKey == "" {
		return nil
	}
	client := clientForProvider(model.Provider)
	client.SetAPIKey(apiKey, model.CustomAPIURL, model.CustomModelName)
	if isUSDCProvider(model.Provider) {
		logger.Infof("Telegram agent: user=%s source=%s tg_model_id=%s ai_model_id=%s provider=%s custom_model=%q (USDC)",
			userID, source, modelID, model.ID, model.Provider, model.CustomModelName)
	} else {
		logger.Infof("Telegram agent: user=%s source=%s tg_model_id=%s ai_model_id=%s provider=%s custom_model=%q",
			userID, source, modelID, model.ID, model.Provider, model.CustomModelName)
	}
	return client
}

// isUSDCProvider returns true for providers that pay per call with USDC (x402 protocol).
func isUSDCProvider(provider string) bool {
	return provider == "claw402"
}

func clientForProvider(provider string) mcp.AIClient {
	client := mcp.NewAIClientByProvider(provider)
	if client == nil {
		client = mcp.NewAIClientByProvider("deepseek")
	}
	return client
}

// ── Status message ────────────────────────────────────────────────────────────

// statusMsg is the single entry-point message shown after /start.
// It checks what's configured and shows either a setup prompt or the ready state.
func statusMsg(st *store.Store, userID string, apiPort int, lang string) string {
	webURL := "http://localhost:3000"

	// Determine what's missing.
	hasModel := false
	if _, err := st.AIModel().GetDefault(userID); err == nil {
		hasModel = true
	}

	hasExchange := false
	if exchanges, err := st.Exchange().List(userID); err == nil {
		for _, e := range exchanges {
			if e.Enabled {
				hasExchange = true
				break
			}
		}
	}

	if !hasModel || !hasExchange {
		missing := ""
		if lang == "zh" {
			if !hasModel {
				missing += "\n❌ AI Model → Settings → AI Models → Add"
			}
			if !hasExchange {
				missing += "\n❌ Exchange → Settings → Exchanges → Add"
			}
			return "⚙️ *Setup required*\n\nOpen the web dashboard to complete setup:\n→ " + webURL + "\n" + missing + "\n\nSend /start when done."
		}
		if !hasModel {
			missing += "\n❌ AI Model → Settings → AI Models → Add"
		}
		if !hasExchange {
			missing += "\n❌ Exchange → Settings → Exchanges → Add"
		}
		return "⚙️ *Setup required*\n\nOpen the web dashboard to complete setup:\n→ " + webURL + "\n" + missing + "\n\nSend /start when done."
	}

	// All configured — show ready state.
	if lang == "zh" {
		return `✅ *NOFX is ready!*

直接说需求，或使用快捷命令：

📊 /positions — 查看持仓
💰 /balanca — 查看余额
🔔 /notify — 开/关交易通知

/help for more · /lang to change language`
	}
	return `✅ *NOFX is ready!*

Just tell me what you want, or use quick commands:

📊 /positions — open positions
💰 /balanca — account balance
🤖 /traders — choose strategy
🔔 /notify — trade alerts on/off

Or ask in plain text:
🤖 "Create a BTC trend strategy and start it"
⏹ "Stop all traders"

/help for more · /lang to change language`
}

// ── Language ──────────────────────────────────────────────────────────────────

func langMenuMsg() string {
	return "🌐 *Choose your language*\n\n1 — English\n2 — Chinese\n\nReply with 1 or 2"
}

// isStartCommand reports whether a Telegram message is a /start command.
func isStartCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	return len(fields) >= 1 && strings.HasPrefix(fields[0], "/start")
}

// parseStartBindCode extracts the bind-code payload from "/start CODE" or
// "/start@botname CODE". Returns "" when no code was supplied.
func parseStartBindCode(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "/start") {
		return ""
	}
	if strings.HasPrefix(fields[1], "@") {
		if len(fields) >= 3 {
			return fields[2]
		}
		return ""
	}
	return fields[1]
}

func parseLangChoice(text string) string {
	switch strings.TrimSpace(text) {
	case "1", "en", "EN", "English", "english":
		return "en"
	case "2", "zh", "ZH", "chinese", "Chinese":
		return "zh"
	}
	return ""
}

// ── Help ──────────────────────────────────────────────────────────────────────

func helpMsg(lang string) string {
	if lang == "zh" {
		return `*NOFX Help*

*Query*
• "Show my positions"
• "What's my balance?"
• "List my traders"

*Create & start*
• "Create a BTC trend strategy and start it"
• "Conservative strategy, BTC and ETH only"

*Control*
• "Start trader"
• "Pause trader"
• "Stop all trading"

*Commands*
/balanca — balance
/positions — open positions
/traders — choose which strategy to view
/use — switch trader (1, 2, all)
/notify — alerts, test, daily, swing
/notify test — all-strategies snapshot preview
/notify daily off — disable daily snapshot (once per day)
/notify swing 5 — uPnL alert threshold

Free (no claw402 AI cost): /balanca /positions /traders /notify
Costs USDC: free-text chat (⏳ AI replies) + autopilot trading cycles

/start — refresh status
/lang  — change language
/help  — show this`
	}
	return `*NOFX Help*

*Query*
• "Show my positions"
• "What's my balance?"
• "List my traders"

*Create & start*
• "Create a BTC trend strategy and start it"
• "Conservative strategy, BTC and ETH only"

*Control*
• "Start trader"
• "Stop trader"
• "Stop all trading"

*Commands*
/balanca — balance
/positions — open positions
/traders — choose which strategy to view
/use — switch trader (1, 2, all)
/notify — alerts, test, daily, swing
/notify test — all-strategies snapshot preview
/notify daily off — disable daily snapshot (once per day)
/notify swing 5 — uPnL alert threshold

Free (no claw402 AI cost): /balanca /positions /traders /notify
Costs USDC: free-text chat (⏳ AI replies) + autopilot trading cycles

/start — refresh status
/lang  — change language
/help  — show this`
}
