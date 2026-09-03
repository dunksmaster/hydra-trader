package telegram

import (
	"encoding/json"
	"fmt"
	"strings"

	"nofx/store"
	"nofx/telegram/agent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func handleFavoriteCommand(bot *tgbotapi.BotAPI, chatID int64, cmd string, st *store.Store, botUserID string, apiPort int) {
	lang := st.TelegramConfig().GetLanguage()
	fields := strings.Fields(strings.TrimSpace(cmd))
	if len(fields) < 2 {
		sendFavoriteList(bot, chatID, st, botUserID, apiPort, lang)
		return
	}

	action := strings.ToLower(fields[1])
	switch action {
	case "list", "ls", "show":
		sendFavoriteList(bot, chatID, st, botUserID, apiPort, lang)
		return
	case "add", "save", "mark":
		if len(fields) < 3 {
			sendFavoriteUsage(bot, chatID, lang)
			return
		}
		token := strings.Join(fields[2:], " ")
		handleFavoriteAdd(bot, chatID, st, botUserID, apiPort, token, lang)
		return
	case "remove", "rm", "del", "delete", "unfav":
		if len(fields) < 3 {
			sendFavoriteUsage(bot, chatID, lang)
			return
		}
		token := strings.Join(fields[2:], " ")
		handleFavoriteRemove(bot, chatID, st, botUserID, apiPort, token, lang)
		return
	default:
		token := strings.Join(fields[1:], " ")
		handleFavoriteAdd(bot, chatID, st, botUserID, apiPort, token, lang)
	}
}

func sendFavoriteList(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, botUserID string, apiPort int, lang string) {
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	client := newQuickClient(apiPort, jwt)
	sendHTMLMsg(bot, chatID, formatFavoriteList(st, client, lang))
}

func sendFavoriteUsage(bot *tgbotapi.BotAPI, chatID int64, lang string) {
	if lang == "zh" {
		sendMsg(bot, chatID, "用法: /fav add 名称 · /fav list · /fav remove 名称")
		return
	}
	sendMsg(bot, chatID, "Usage: /fav add NAME · /fav list · /fav remove NAME")
}

func handleFavoriteAdd(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, botUserID string, apiPort int, token, lang string) {
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	client := newQuickClient(apiPort, jwt)
	match, err := resolveCopyTraderToken(client, token)
	if err != nil {
		sendMsg(bot, chatID, err.Error())
		return
	}
	if err := st.TelegramConfig().AddFavoriteTraderID(match.TraderID); err != nil {
		sendMsg(bot, chatID, "Could not save favorite: "+err.Error())
		return
	}
	if lang == "zh" {
		sendHTMLMsg(bot, chatID, fmt.Sprintf("⭐ 已收藏 <b>%s</b>", escapeHTML(match.Name)))
		return
	}
	sendHTMLMsg(bot, chatID, fmt.Sprintf("⭐ Favorited <b>%s</b>", escapeHTML(match.Name)))
}

func handleFavoriteRemove(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, botUserID string, apiPort int, token, lang string) {
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	client := newQuickClient(apiPort, jwt)
	match, err := resolveCopyTraderToken(client, token)
	if err != nil {
		sendMsg(bot, chatID, err.Error())
		return
	}
	if err := st.TelegramConfig().RemoveFavoriteTraderID(match.TraderID); err != nil {
		sendMsg(bot, chatID, err.Error())
		return
	}
	if lang == "zh" {
		sendHTMLMsg(bot, chatID, fmt.Sprintf("已取消收藏 <b>%s</b>", escapeHTML(match.Name)))
		return
	}
	sendHTMLMsg(bot, chatID, fmt.Sprintf("Removed <b>%s</b> from favorites", escapeHTML(match.Name)))
}

func formatFavoriteList(st *store.Store, client *quickClient, lang string) string {
	ids := st.TelegramConfig().GetFavoriteTraderIDs()
	if len(ids) == 0 {
		if lang == "zh" {
			return "⭐ <b>收藏</b>\n\n暂无。/fav add 名称"
		}
		return "⭐ <b>Favorites</b>\n\nNone yet. /fav add NAME"
	}

	title := fmt.Sprintf("⭐ <b>Favorites (%d)</b>", len(ids))
	if lang == "zh" {
		title = fmt.Sprintf("⭐ <b>收藏 (%d)</b>", len(ids))
	}
	lines := []string{title, ""}

	perf, _ := fetchCopyTraderPerformance(client, favoriteSet(ids))
	byID := make(map[string]CopyTraderPerf, len(perf))
	for _, r := range perf {
		byID[r.TraderID] = r
	}
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			lines = append(lines, "• "+formatFavoriteSummaryLine(r))
			continue
		}
		lines = append(lines, "• <code>"+escapeHTML(shortTraderID(id))+"</code>")
	}

	hint := "/fav remove NAME — remove"
	if lang == "zh" {
		hint = "/fav remove 名称 — 取消收藏"
	}
	lines = append(lines, "", "<i>"+hint+"</i>")
	return strings.Join(lines, "\n")
}

func formatFavoriteSummaryLine(r CopyTraderPerf) string {
	winPct := "—"
	if r.Stats.TotalTrades > 0 {
		winPct = fmt.Sprintf("%.0f%%", r.Stats.WinRate)
	}
	layerTag := fmt.Sprintf("L%d", r.Layer)
	return fmt.Sprintf("<b>%s</b> — %s · %s · %s",
		escapeHTML(r.Name), formatPnLMoney(r.Stats.TotalPnL), winPct, layerTag)
}

type copyTraderMatch struct {
	TraderID string
	Name     string
}

func resolveCopyTraderToken(client *quickClient, token string) (copyTraderMatch, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return copyTraderMatch{}, fmt.Errorf("name or id required")
	}
	copyTraders, err := fetchCopyTraders(client)
	if err != nil {
		return copyTraderMatch{}, err
	}
	if len(copyTraders) == 0 {
		return copyTraderMatch{}, fmt.Errorf("no copy traders found")
	}

	tokens := strings.Fields(strings.ToLower(token))
	var matches []copyTraderMatch
	for _, tr := range copyTraders {
		id := strings.TrimSpace(fmt.Sprint(tr["trader_id"], tr["id"]))
		name := strings.TrimSpace(fmt.Sprint(tr["trader_name"], tr["name"]))
		if id == "" {
			continue
		}
		if traderTokenMatches(id, name, tr, client, tokens) {
			matches = append(matches, copyTraderMatch{TraderID: id, Name: name})
		}
	}
	if len(matches) == 0 {
		return copyTraderMatch{}, fmt.Errorf("no copy trader matching %q — try /perf to list names", token)
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return copyTraderMatch{}, fmt.Errorf("ambiguous match (%s) — be more specific", strings.Join(names, ", "))
	}
	return matches[0], nil
}

func traderTokenMatches(id, name string, tr map[string]any, client *quickClient, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	lowerID := strings.ToLower(id)
	lowerName := strings.ToLower(name)
	leader := strings.ToLower(leaderAddressForTrader(client, tr))
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if !singleTokenMatchesCopyTrader(lowerID, lowerName, leader, tok) {
			return false
		}
	}
	return true
}

func singleTokenMatchesCopyTrader(lowerID, lowerName, leader, tok string) bool {
	if lowerID == tok || strings.HasSuffix(lowerID, tok) || strings.Contains(lowerID, tok) {
		return true
	}
	if lowerName != "" && tokenMatchesCopyTraderName(lowerName, tok) {
		return true
	}
	return leader != "" && strings.Contains(leader, tok)
}

func leaderAddressForTrader(client *quickClient, tr map[string]any) string {
	strategyID := fmt.Sprint(tr["strategy_id"])
	if strategyID == "" || strategyID == "<nil>" || client == nil {
		return ""
	}
	stBody, err := client.get("/api/strategies/" + strategyID)
	if err != nil {
		return ""
	}
	var stMap map[string]any
	if json.Unmarshal(stBody, &stMap) != nil {
		return ""
	}
	cfg, _ := stMap["config"].(map[string]any)
	copyCfg, _ := cfg["copy_config"].(map[string]any)
	return strings.TrimSpace(fmt.Sprint(copyCfg["leader_address"]))
}

func shortTraderID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "…"
}
