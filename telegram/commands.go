package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nofx/store"
	"nofx/telegram/agent"
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
		TraderID  string `json:"trader_id"`
		TraderName string `json:"trader_name"`
		IsRunning bool   `json:"is_running"`
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

func handleQuickCommand(bot *tgbotapi.BotAPI, chatID int64, cmd string, st *store.Store, botUserID string, apiPort int) {
	lang := st.TelegramConfig().GetLanguage()

	switch normalizeQuickCommand(cmd) {
	case "notify", "njoftimet", "njoftime":
		handleNotifyCommand(bot, chatID, cmd, st, lang)
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
		reply, err := formatBalance(client, lang)
		if err != nil {
			sendMsg(bot, chatID, "Could not fetch balance: "+err.Error())
			return
		}
		sendMarkdownMsg(bot, chatID, reply)
	case "positions", "pozicione", "pozicionet", "pos":
		reply, err := formatPositions(client, lang)
		if err != nil {
			sendMsg(bot, chatID, "Could not fetch positions: "+err.Error())
			return
		}
		sendMarkdownMsg(bot, chatID, reply)
	case "traders", "tregtar":
		reply, err := formatTraders(client, lang)
		if err != nil {
			sendMsg(bot, chatID, "Could not fetch traders: "+err.Error())
			return
		}
		sendMarkdownMsg(bot, chatID, reply)
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

func handleNotifyCommand(bot *tgbotapi.BotAPI, chatID int64, text string, st *store.Store, lang string) {
	fields := strings.Fields(strings.TrimSpace(text))
	action := ""
	if len(fields) >= 2 {
		action = strings.ToLower(fields[1])
	}

	switch action {
	case "on", "yes", "1", "po":
		_ = st.TelegramConfig().SetNotifyEnabled(true)
		sendMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "off", "no", "0", "jo":
		_ = st.TelegramConfig().SetNotifyEnabled(false)
		sendMsg(bot, chatID, notifyStatusMsg(st, lang))
	default:
		sendMarkdownMsg(bot, chatID, notifyStatusMsg(st, lang))
	}
}

func notifyStatusMsg(st *store.Store, lang string) string {
	enabled := st.TelegramConfig().IsNotifyEnabled()
	if lang == "zh" {
		if enabled {
			return "🔔 *通知已开启*\n\n开仓/平仓时会自动推送消息。\n\n/notify off — 关闭\n/notify on — 开启"
		}
		return "🔕 *通知已关闭*\n\n/notify on — 开启自动推送"
	}
	if enabled {
		return "🔔 *Notifications ON*\n\nYou'll get alerts when positions open or close.\n\n/notify off — turn off\n/notify on — turn on"
	}
	return "🔕 *Notifications OFF*\n\n/notify on — turn on auto alerts"
}

func formatBalance(c *quickClient, lang string) (string, error) {
	traderID, traderName, err := defaultTraderID(c)
	if err != nil {
		return "", err
	}
	body, err := c.get("/api/account?trader_id=" + traderID)
	if err != nil {
		return "", err
	}
	var acct map[string]any
	if err := json.Unmarshal(body, &acct); err != nil {
		return "", err
	}

	equity := num(acct["total_equity"])
	available := num(acct["available_balance"])
	pnl := num(acct["total_pnl"])
	pnlPct := num(acct["total_pnl_pct"])

	pnlSign := "+"
	if pnl < 0 {
		pnlSign = ""
	}

	if lang == "zh" {
		return fmt.Sprintf(
			"💰 *账户余额*\n\n*交易员:* %s\n*权益:* $%.2f\n*可用:* $%.2f\n*盈亏:* %s$%.2f (%.2f%%)",
			traderName, equity, available, pnlSign, pnl, pnlPct,
		), nil
	}
	return fmt.Sprintf(
		"💰 *Balance*\n\n*Trader:* %s\n*Equity:* $%.2f\n*Available:* $%.2f\n*PnL:* %s$%.2f (%.2f%%)",
		traderName, equity, available, pnlSign, pnl, pnlPct,
	), nil
}

func formatPositions(c *quickClient, lang string) (string, error) {
	traderID, traderName, err := defaultTraderID(c)
	if err != nil {
		return "", err
	}
	body, err := c.get("/api/positions?trader_id=" + traderID)
	if err != nil {
		return "", err
	}
	var positions []map[string]any
	if err := json.Unmarshal(body, &positions); err != nil {
		return "", err
	}

	if len(positions) == 0 {
		if lang == "zh" {
			return fmt.Sprintf("📊 *持仓* (%s)\n\n暂无持仓。", traderName), nil
		}
		return fmt.Sprintf("📊 *Positions* (%s)\n\nNo open positions.", traderName), nil
	}

	var sb strings.Builder
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("📊 *持仓* (%s)\n\n", traderName))
	} else {
		sb.WriteString(fmt.Sprintf("📊 *Positions* (%s)\n\n", traderName))
	}

	for i, p := range positions {
		symbol, _ := p["symbol"].(string)
		side, _ := p["side"].(string)
		if side == "" {
			if ps, ok := p["position_side"].(string); ok {
				side = ps
			}
		}
		qty := num(p["quantity"])
		if qty == 0 {
			qty = num(p["position_amt"])
		}
		entry := num(p["entry_price"])
		upnl := num(p["unrealized_pnl"])
		lev := num(p["leverage"])
		if lev == 0 {
			lev = 1
		}

		sign := "+"
		if upnl < 0 {
			sign = ""
		}
		sb.WriteString(fmt.Sprintf("%d. *%s* %s · %.4f @ $%.4f · %dx · uPnL %s$%.2f\n",
			i+1, strings.ToUpper(side), trimSymbol(symbol), qty, entry, int(lev), sign, upnl))
	}
	return strings.TrimSpace(sb.String()), nil
}

func formatTraders(c *quickClient, lang string) (string, error) {
	body, err := c.get("/api/my-traders")
	if err != nil {
		return "", err
	}
	var traders []struct {
		TraderID   string  `json:"trader_id"`
		TraderName string  `json:"trader_name"`
		IsRunning  bool    `json:"is_running"`
		InitialBalance float64 `json:"initial_balance"`
	}
	if err := json.Unmarshal(body, &traders); err != nil {
		return "", err
	}
	if len(traders) == 0 {
		if lang == "zh" {
			return "🤖 *交易员*\n\n暂无配置。", nil
		}
		return "🤖 *Traders*\n\nNo traders configured.", nil
	}

	var sb strings.Builder
	if lang == "zh" {
		sb.WriteString("🤖 *交易员*\n\n")
	} else {
		sb.WriteString("🤖 *Traders*\n\n")
	}
	for i, t := range traders {
		status := "⏹ stopped"
		if t.IsRunning {
			status = "▶️ running"
		}
		sb.WriteString(fmt.Sprintf("%d. *%s* — %s\n", i+1, t.TraderName, status))
	}
	return strings.TrimSpace(sb.String()), nil
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func trimSymbol(s string) string {
	s = strings.ToUpper(s)
	return strings.TrimSuffix(s, "USDT")
}
