package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"nofx/store"
	"nofx/telegram/agent"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (c *quickClient) post(path string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func closePositionAPI(c *quickClient, traderID, symbol, side string) error {
	path := fmt.Sprintf("/api/traders/%s/close-position", url.PathEscape(traderID))
	_, err := c.post(path, map[string]string{
		"symbol": symbol,
		"side":   strings.ToUpper(side),
	})
	return err
}

func handleCloseCommand(bot *tgbotapi.BotAPI, chatID int64, cmd string, st *store.Store, lang string, botUserID string, apiPort int) {
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	client := newQuickClient(apiPort, jwt)
	portfolios, err := fetchPortfoliosForSelection(st, client)
	if err != nil {
		sendMsg(bot, chatID, "Could not fetch positions: "+err.Error())
		return
	}

	fields := strings.Fields(strings.TrimSpace(cmd))
	args := fields[1:]
	if len(args) == 0 {
		sendHTMLMsg(bot, chatID, formatCloseHelp(portfolios, lang))
		return
	}

	arg0 := strings.ToLower(args[0])
	if arg0 == "all" {
		handleCloseAll(bot, chatID, portfolios, client, lang)
		return
	}

	var sideHint string
	var venueHint string
	symbolQuery := args[0]
	if len(args) >= 2 {
		symbolQuery = args[0]
		sideHint = strings.ToLower(args[1])
	}
	if len(args) >= 3 {
		venueHint = strings.ToLower(args[2])
	}

	matches := findCloseTargets(portfolios, symbolQuery, sideHint, venueHint)
	if len(matches) == 0 {
		if lang == "zh" {
			sendHTMLMsg(bot, chatID, fmt.Sprintf("❌ 未找到持仓 <b>%s</b>。发送 /positions 查看，或 /close all 全部平仓。", escapeHTML(symbolQuery)))
		} else {
			sendHTMLMsg(bot, chatID, fmt.Sprintf("❌ No open position matching <b>%s</b>. Try /positions or /close all.", escapeHTML(symbolQuery)))
		}
		return
	}
	if len(matches) > 1 && sideHint == "" {
		sendHTMLMsg(bot, chatID, formatCloseAmbiguous(matches, lang))
		return
	}

	var sb strings.Builder
	if lang == "zh" {
		sb.WriteString("🔻 <b>平仓中…</b>\n\n")
	} else {
		sb.WriteString("🔻 <b>Closing…</b>\n\n")
	}
	ok, fail := 0, 0
	for _, m := range matches {
		err := closePositionAPI(client, m.TraderID, m.Symbol, m.Side)
		sym := displaySymbol(m.Symbol)
		if strings.HasPrefix(strings.ToLower(m.Symbol), "xyz:") {
			sym = m.Symbol
		}
		if err != nil {
			fail++
			sb.WriteString(fmt.Sprintf("❌ %s %s — %s\n", escapeHTML(sym), strings.ToUpper(m.Side), escapeHTML(err.Error())))
		} else {
			ok++
			sb.WriteString(fmt.Sprintf("✅ %s %s closed\n", escapeHTML(sym), strings.ToUpper(m.Side)))
		}
	}
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("\n完成：%d 成功 · %d 失败", ok, fail))
	} else {
		sb.WriteString(fmt.Sprintf("\nDone: %d closed · %d failed", ok, fail))
	}
	sendHTMLMsg(bot, chatID, sb.String())
}

type closeTarget struct {
	TraderID   string
	TraderName string
	Symbol     string
	Side       string
	Exchange   string
}

func findCloseTargets(portfolios []TraderPortfolio, symbolQuery, sideHint, venueHint string) []closeTarget {
	var out []closeTarget
	for _, g := range groupPositionsByVenue(portfolios) {
		if venueHint != "" && !venueMatchesQuery(g.Exchange, venueHint) {
			continue
		}
		for _, vp := range g.Positions {
			if !symbolMatchesQuery(vp.Symbol, symbolQuery) {
				continue
			}
			if sideHint != "" && sideHint != vp.Side {
				continue
			}
			out = append(out, closeTarget{
				TraderID:   vp.CloseTraderID,
				TraderName: g.Label,
				Symbol:     vp.Symbol,
				Side:       vp.Side,
				Exchange:   g.Exchange,
			})
		}
	}
	return out
}

func symbolMatchesQuery(posSymbol, query string) bool {
	query = strings.ToUpper(strings.TrimSpace(query))
	if query == "" {
		return false
	}
	pos := strings.ToUpper(strings.TrimSpace(posSymbol))
	if pos == query {
		return true
	}
	if displaySymbol(pos) == query {
		return true
	}
	if strings.TrimSuffix(pos, "USDT") == query {
		return true
	}
	if strings.HasPrefix(strings.ToLower(pos), "xyz:") {
		base := strings.TrimPrefix(pos, "XYZ:")
		if base == query || strings.Contains(base, query) {
			return true
		}
	}
	return false
}

func handleCloseAll(bot *tgbotapi.BotAPI, chatID int64, portfolios []TraderPortfolio, client *quickClient, lang string) {
	var targets []closeTarget
	for _, g := range groupPositionsByVenue(portfolios) {
		for _, vp := range g.Positions {
			targets = append(targets, closeTarget{
				TraderID:   vp.CloseTraderID,
				TraderName: g.Label,
				Symbol:     vp.Symbol,
				Side:       vp.Side,
				Exchange:   g.Exchange,
			})
		}
	}
	if len(targets) == 0 {
		if lang == "zh" {
			sendMsg(bot, chatID, "暂无持仓可平。")
		} else {
			sendMsg(bot, chatID, "No open positions to close.")
		}
		return
	}
	var sb strings.Builder
	if lang == "zh" {
		sb.WriteString("🔻 <b>全部平仓…</b>\n\n")
	} else {
		sb.WriteString("🔻 <b>Closing all…</b>\n\n")
	}
	ok := 0
	for _, m := range targets {
		err := closePositionAPI(client, m.TraderID, m.Symbol, m.Side)
		sym := displaySymbol(m.Symbol)
		if strings.HasPrefix(strings.ToLower(m.Symbol), "xyz:") {
			sym = m.Symbol
		}
		if err != nil {
			sb.WriteString(fmt.Sprintf("❌ %s %s — %s\n", escapeHTML(sym), strings.ToUpper(m.Side), escapeHTML(err.Error())))
		} else {
			ok++
			sb.WriteString(fmt.Sprintf("✅ %s %s\n", escapeHTML(sym), strings.ToUpper(m.Side)))
		}
	}
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("\n已平 %d / %d", ok, len(targets)))
	} else {
		sb.WriteString(fmt.Sprintf("\nClosed %d / %d", ok, len(targets)))
	}
	sendHTMLMsg(bot, chatID, sb.String())
}

func formatCloseHelp(portfolios []TraderPortfolio, lang string) string {
	var sb strings.Builder
	if lang == "zh" {
		sb.WriteString("<b>🔻 平仓</b>\n\n")
		sb.WriteString("1. 发送 <code>/positions</code>\n")
		sb.WriteString("2. 点击该持仓下方的 <b>Close</b> 按钮\n")
		sb.WriteString("   或点击卡片里的 <code>/close_xxxx</code> 链接\n\n")
		sb.WriteString("快捷：<code>/close BTC</code> · <code>/close all</code> 全部平仓\n\n")
	} else {
		sb.WriteString("<b>🔻 Close a position</b>\n\n")
		sb.WriteString("1. Send <code>/positions</code>\n")
		sb.WriteString("2. Tap the <b>Close</b> button under that coin\n")
		sb.WriteString("   or tap the <code>/close_xxxx</code> link in the same card\n\n")
		sb.WriteString("Shortcut: <code>/close BTC</code> · <code>/close all</code> closes everything\n\n")
	}
	count := 0
	for _, g := range groupPositionsByVenue(portfolios) {
		for _, vp := range g.Positions {
			if vp.CloseTraderID == "" {
				continue
			}
			count++
			sym := displaySymbol(vp.Symbol)
			if strings.HasPrefix(strings.ToLower(vp.Symbol), "xyz:") {
				sym = vp.Symbol
			}
			cmd := closeCommand(mintCloseTokenID(vp.CloseTraderID, vp.Symbol, vp.Side))
			if count == 1 {
				if lang == "zh" {
					sb.WriteString("<b>当前持仓：</b>\n")
				} else {
					sb.WriteString("<b>Open now:</b>\n")
				}
			}
			sb.WriteString(fmt.Sprintf("• <b>%s</b> %s", escapeHTML(sym), escapeHTML(strings.ToUpper(vp.Side))))
			if g.Label != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", escapeHTML(g.Label)))
			}
			sb.WriteString(fmt.Sprintf("\n  <code>%s</code>\n", escapeHTML(cmd)))
		}
	}
	if count == 0 {
		if lang == "zh" {
			sb.WriteString("当前无持仓。")
		} else {
			sb.WriteString("No open positions right now.")
		}
	}
	return sb.String()
}

func formatCloseAmbiguous(matches []closeTarget, lang string) string {
	var sb strings.Builder
	if lang == "zh" {
		sb.WriteString("❌ 多个匹配 — 请用下方命令之一：\n")
	} else {
		sb.WriteString("❌ Multiple matches — use one of these:\n")
	}
	for _, m := range matches {
		sym := displaySymbol(m.Symbol)
		if strings.HasPrefix(strings.ToLower(m.Symbol), "xyz:") {
			sym = m.Symbol
		}
		var cmd string
		if m.TraderID != "" {
			cmd = closeCommand(mintCloseTokenID(m.TraderID, m.Symbol, m.Side))
		} else {
			cmd = fmt.Sprintf("/close %s %s", sym, m.Side)
		}
		sb.WriteString(fmt.Sprintf("• <code>%s</code>\n", escapeHTML(cmd)))
	}
	return sb.String()
}
