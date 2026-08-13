package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nofx/events"
	"nofx/store"
	"nofx/telegram/agent"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type quickClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newQuickClient(apiPort int, jwt string) *quickClient {
	return &quickClient{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", apiPort),
		token:   jwt,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *quickClient) get(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func defaultTraderID(c *quickClient) (string, string, error) {
	body, err := c.get("/api/my-traders")
	if err != nil {
		return "", "", err
	}
	var traders []struct {
		TraderID   string `json:"trader_id"`
		TraderName string `json:"trader_name"`
		IsRunning  bool   `json:"is_running"`
	}
	if err := json.Unmarshal(body, &traders); err != nil {
		return "", "", err
	}
	if len(traders) == 0 {
		return "", "", fmt.Errorf("no traders configured")
	}
	for _, t := range traders {
		if t.IsRunning {
			return t.TraderID, t.TraderName, nil
		}
	}
	return traders[0].TraderID, traders[0].TraderName, nil
}

func fetchAccountSnapshot(c *quickClient) (AccountSnapshot, error) {
	traderID, traderName, err := defaultTraderID(c)
	if err != nil {
		return AccountSnapshot{}, err
	}
	body, err := c.get("/api/account?trader_id=" + traderID)
	if err != nil {
		return AccountSnapshot{}, err
	}
	var acct map[string]any
	if err := json.Unmarshal(body, &acct); err != nil {
		return AccountSnapshot{}, err
	}
	snap := ParseAccountSnapshot(acct, traderName)
	if snap.PositionCount == 0 {
		// sync count from positions endpoint if account omits it
		if posBody, err := c.get("/api/positions?trader_id=" + traderID); err == nil {
			var positions []map[string]any
			if json.Unmarshal(posBody, &positions) == nil {
				snap.PositionCount = len(positions)
			}
		}
	}
	return snap, nil
}

func fetchAccountAndPositions(c *quickClient) (AccountSnapshot, []map[string]any, error) {
	traderID, traderName, err := defaultTraderID(c)
	if err != nil {
		return AccountSnapshot{}, nil, err
	}
	acctBody, err := c.get("/api/account?trader_id=" + traderID)
	if err != nil {
		return AccountSnapshot{}, nil, err
	}
	var acct map[string]any
	if err := json.Unmarshal(acctBody, &acct); err != nil {
		return AccountSnapshot{}, nil, err
	}
	posBody, err := c.get("/api/positions?trader_id=" + traderID)
	if err != nil {
		return AccountSnapshot{}, nil, err
	}
	var positions []map[string]any
	if err := json.Unmarshal(posBody, &positions); err != nil {
		return AccountSnapshot{}, nil, err
	}
	snap := ParseAccountSnapshot(acct, traderName)
	snap.PositionCount = len(positions)
	return snap, positions, nil
}

func handleQuickCommand(bot *tgbotapi.BotAPI, chatID int64, cmd string, st *store.Store, botUserID string, apiPort int) {
	lang := st.TelegramConfig().GetLanguage()

	switch normalizeQuickCommand(cmd) {
	case "notify", "njoftimet", "njoftime":
		handleNotifyCommand(bot, chatID, cmd, st, lang, botUserID, apiPort)
		return
	}

	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	client := newQuickClient(apiPort, jwt)

	switch normalizeQuickCommand(cmd) {
	case "balance", "balanca", "balanc":
		snap, err := fetchAccountSnapshot(client)
		if err != nil {
			sendMsg(bot, chatID, "Could not fetch balance: "+err.Error())
			return
		}
		title := "💰 Portfolio"
		if lang == "zh" {
			title = "💰 账户"
		}
		sendHTMLMsg(bot, chatID, formatPortfolioBlock(snap, lang, title))
	case "positions", "pozicione", "pozicionet", "pos":
		snap, positions, err := fetchAccountAndPositions(client)
		if err != nil {
			sendMsg(bot, chatID, "Could not fetch positions: "+err.Error())
			return
		}
		sendHTMLMsg(bot, chatID, formatRichPositions(snap, positions, lang))
	case "traders", "tregtar":
		reply, err := formatTraders(client, lang)
		if err != nil {
			sendMsg(bot, chatID, "Could not fetch traders: "+err.Error())
			return
		}
		sendHTMLMsg(bot, chatID, reply)
	default:
		return
	}
}

func normalizeQuickCommand(cmd string) string {
	cmd = strings.TrimSpace(strings.ToLower(cmd))
	cmd = strings.TrimPrefix(cmd, "/")
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	return cmd
}

func isQuickCommand(text string) bool {
	if !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	base := normalizeQuickCommand(fields[0])
	switch base {
	case "balance", "balanca", "balanc", "positions", "pozicione", "pozicionet", "pos",
		"traders", "tregtar", "notify", "njoftimet", "njoftime":
		return true
	}
	return false
}

func matchNLQuickIntent(text string) string {
	norm := strings.ToLower(strings.TrimSpace(text))
	norm = strings.TrimSuffix(norm, "?")
	norm = strings.TrimSuffix(norm, ".")
	norm = strings.TrimSpace(norm)
	if norm == "" {
		return ""
	}
	switch {
	case containsAny(norm,
		"show my positions", "show positions", "my positions", "open positions",
		"what are my positions", "what's my positions", "list my positions",
		"current positions", "pozicionet e mia", "pozicionet", "pozicione",
	):
		return "positions"
	case containsAny(norm,
		"show my balance", "my balance", "what's my balance", "what is my balance",
		"account balance", "show balance", "balanca ime", "balanca", "balanc",
	):
		return "balance"
	case containsAny(norm,
		"list my traders", "show my traders", "my traders", "list traders",
		"tregtarët e mi", "tregtar",
	):
		return "traders"
	}
	return ""
}

func containsAny(s string, phrases ...string) bool {
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func handleNotifyCommand(bot *tgbotapi.BotAPI, chatID int64, text string, st *store.Store, lang string, botUserID string, apiPort int) {
	fields := strings.Fields(strings.TrimSpace(text))
	action := ""
	if len(fields) >= 2 {
		action = strings.ToLower(fields[1])
	}

	switch action {
	case "on", "yes", "1", "po":
		_ = st.TelegramConfig().SetNotifyEnabled(true)
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "off", "no", "0", "jo":
		_ = st.TelegramConfig().SetNotifyEnabled(false)
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "daily", "digest":
		sub := "on"
		if len(fields) >= 3 {
			sub = strings.ToLower(fields[2])
		}
		if sub == "off" {
			_ = st.TelegramConfig().SetDigestEnabled(false)
		} else {
			_ = st.TelegramConfig().SetDigestEnabled(true)
		}
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "swing":
		if len(fields) >= 3 {
			if v, err := strconv.ParseFloat(fields[2], 64); err == nil {
				_ = st.TelegramConfig().SetPnlSwingThreshold(v)
			}
		}
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "test", "prove", "provo":
		jwt, err := agent.GenerateBotToken(botUserID)
		if err != nil {
			sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
			return
		}
		sendNotifyTest(bot, chatID, st, lang, newQuickClient(apiPort, jwt))
	default:
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	}
}

func sendNotifyTest(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, lang string, client *quickClient) {
	if lang == "zh" {
		sendHTMLMsg(bot, chatID, "🔔 <b>测试通知</b>\n\n如果你看到这条消息，Telegram 推送正常。")
	} else {
		sendHTMLMsg(bot, chatID, "🔔 <b>Test notification</b>\n\nIf you see this, Telegram push is working.")
	}

	snap, positions, err := fetchAccountAndPositions(client)
	if err != nil {
		sendMsg(bot, chatID, "Could not load open positions: "+err.Error())
		return
	}

	title := "📊 Your open positions now"
	if lang == "zh" {
		title = "📊 当前持仓"
	}
	sendHTMLMsg(bot, chatID, "<b>"+escapeHTML(title)+"</b>\n\n"+formatRichPositions(snap, positions, lang))

	sample := formatTradeAlertRich(st, events.TradeEvent{
		TraderID: "demo",
		Symbol:   "BTCUSDT",
		Side:     "LONG",
		Action:   "open_long",
		Quantity: 0.01,
		Price:    95000,
		Leverage: 10,
		OrderID:  "test-sample",
	}, lang, nil)
	label := "Example of a real trade alert:"
	if lang == "zh" {
		label = "真实成交通知示例:"
	}
	sendHTMLMsg(bot, chatID, "<b>"+escapeHTML(label)+"</b>\n\n"+sample)
}

func notifyStatusMsg(st *store.Store, lang string) string {
	enabled := st.TelegramConfig().IsNotifyEnabled()
	digestOn := st.TelegramConfig().IsDigestEnabled()
	threshold := st.TelegramConfig().GetPnlSwingThreshold()

	if lang == "zh" {
		if enabled {
			return fmt.Sprintf(
				"🔔 <b>通知已开启</b>\n\n"+
					"每日快照: %s · 波动阈值: %s\n\n"+
					"/notify test — 测试\n/notify off — 关闭\n/notify daily off — 关闭每日\n/notify swing 5 — 设置阈值",
				onOff(digestOn), formatMoney(threshold))
		}
		return "🔕 <b>通知已关闭</b>\n\n/notify on — 开启"
	}
	if enabled {
		return fmt.Sprintf(
			"🔔 <b>Notifications ON</b>\n\n"+
				"Daily digest: %s · Swing threshold: %s\n\n"+
				"/notify test — test push\n/notify off — turn off\n/notify daily off — disable daily\n/notify swing 5 — set threshold",
			onOff(digestOn), formatMoney(threshold))
	}
	return "🔕 <b>Notifications OFF</b>\n\n/notify on — turn on auto alerts"
}

func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

func formatTraders(c *quickClient, lang string) (string, error) {
	body, err := c.get("/api/my-traders")
	if err != nil {
		return "", err
	}
	var traders []struct {
		TraderID       string  `json:"trader_id"`
		TraderName     string  `json:"trader_name"`
		IsRunning      bool    `json:"is_running"`
		InitialBalance float64 `json:"initial_balance"`
	}
	if err := json.Unmarshal(body, &traders); err != nil {
		return "", err
	}
	if len(traders) == 0 {
		if lang == "zh" {
			return "🤖 <b>交易员</b>\n\n暂无配置。", nil
		}
		return "🤖 <b>Traders</b>\n\nNo traders configured.", nil
	}

	var sb strings.Builder
	if lang == "zh" {
		sb.WriteString("🤖 <b>交易员</b>\n\n")
	} else {
		sb.WriteString("🤖 <b>Traders</b>\n\n")
	}
	for i, t := range traders {
		status := "⏹ stopped"
		if t.IsRunning {
			status = "▶️ running"
		}
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b> — %s\n", i+1, escapeHTML(t.TraderName), status))
	}
	return strings.TrimSpace(sb.String()), nil
}
