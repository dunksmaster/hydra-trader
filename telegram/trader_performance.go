package telegram

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"nofx/store"
	"nofx/telegram/agent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// CopyTraderPerf holds closed-trade performance for one copy bot.
type CopyTraderPerf struct {
	TraderID   string
	Name       string
	Layer      int
	Paused     bool
	Running    bool
	Stats      TradingStats
	Streak     string
	FetchErr   string
	IsFavorite bool
}

func handleTraderPerformanceCommand(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, botUserID string, apiPort int) {
	lang := st.TelegramConfig().GetLanguage()
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	favs := favoriteSet(st.TelegramConfig().GetFavoriteTraderIDs())
	sendHTMLMsg(bot, chatID, formatTraderPerformanceReport(newQuickClient(apiPort, jwt), lang, favs))
}

func favoriteSet(ids []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func fetchCopyTraderPerformance(client *quickClient, favorites map[string]struct{}) ([]CopyTraderPerf, error) {
	copyTraders, err := fetchCopyTraders(client)
	if err != nil {
		return nil, err
	}
	if len(copyTraders) == 0 {
		return nil, nil
	}

	out := make([]CopyTraderPerf, len(copyTraders))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, tr := range copyTraders {
		wg.Add(1)
		go func(i int, tr map[string]any) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = buildCopyTraderPerf(client, tr, favorites)
		}(i, tr)
	}
	wg.Wait()

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsFavorite != out[j].IsFavorite {
			return out[i].IsFavorite
		}
		return out[i].Stats.TotalPnL > out[j].Stats.TotalPnL
	})
	return out, nil
}

func buildCopyTraderPerf(client *quickClient, tr map[string]any, favorites map[string]struct{}) CopyTraderPerf {
	id := strings.TrimSpace(fmt.Sprint(tr["trader_id"], tr["id"]))
	name := copyTraderDisplayName(strings.TrimSpace(fmt.Sprint(tr["trader_name"], tr["name"])))
	running := fmt.Sprint(tr["is_running"]) == "true"
	layer := copyLayerFromTraderRow(client, tr)
	paused := copyPausedFromTraderRow(client, tr)

	row := CopyTraderPerf{
		TraderID: id,
		Name:     name,
		Layer:    layer,
		Paused:   paused,
		Running:  running,
	}
	if id != "" {
		if _, ok := favorites[id]; ok {
			row.IsFavorite = true
		}
		row.Stats = fetchTradingStats(client, id)
		if row.Stats.TotalTrades > 0 {
			info := TraderInfo{TraderID: id, TraderName: name}
			hist := fetchTraderHistory(client, info, 20)
			if hist.FetchErr == "" {
				row.Streak = formatTradeStreak(hist.Trades)
			}
		}
	}
	return row
}

func copyLayerFromTraderRow(client *quickClient, tr map[string]any) int {
	switch v := tr["copy_layer"].(type) {
	case float64:
		if v >= 1 && v <= 3 {
			return int(v)
		}
	case int:
		if v >= 1 && v <= 3 {
			return v
		}
	}
	strategyID := fmt.Sprint(tr["strategy_id"])
	if strategyID == "" || strategyID == "<nil>" {
		return 2
	}
	return copyLayerFromStrategy(client, strategyID)
}

func copyPausedFromTraderRow(client *quickClient, tr map[string]any) bool {
	if v, ok := tr["copy_paused"].(bool); ok {
		return v
	}
	strategyID := fmt.Sprint(tr["strategy_id"])
	if strategyID == "" || strategyID == "<nil>" {
		return false
	}
	return copyPausedFromStrategy(client, strategyID)
}

func copyPausedFromStrategy(client *quickClient, strategyID string) bool {
	if client == nil || strategyID == "" {
		return false
	}
	stBody, err := client.get("/api/strategies/" + strategyID)
	if err != nil {
		return false
	}
	var stMap map[string]any
	if json.Unmarshal(stBody, &stMap) != nil {
		return false
	}
	cfg, _ := stMap["config"].(map[string]any)
	copyCfg, _ := cfg["copy_config"].(map[string]any)
	if v, ok := copyCfg["copy_paused"].(bool); ok {
		return v
	}
	return false
}

func copyLayerFromStrategy(client *quickClient, strategyID string) int {
	if client == nil || strategyID == "" {
		return 2
	}
	stBody, err := client.get("/api/strategies/" + strategyID)
	if err != nil {
		return 2
	}
	var stMap map[string]any
	if json.Unmarshal(stBody, &stMap) != nil {
		return 2
	}
	cfg, _ := stMap["config"].(map[string]any)
	copyCfg, _ := cfg["copy_config"].(map[string]any)
	return copyLayerFromConfig(copyCfg)
}

func formatTradeStreak(trades []ClosedTrade) string {
	if len(trades) == 0 {
		return "—"
	}
	ordered := append([]ClosedTrade(nil), trades...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ExitTime > ordered[j].ExitTime
	})
	firstWin := ordered[0].RealizedPnL > 0
	count := 0
	for _, t := range ordered {
		win := t.RealizedPnL > 0
		if win != firstWin {
			break
		}
		count++
	}
	if count == 0 {
		return "—"
	}
	if firstWin {
		return fmt.Sprintf("%dW", count)
	}
	return fmt.Sprintf("%dL", count)
}

func formatTraderPerformanceReport(client *quickClient, lang string, favorites map[string]struct{}) string {
	rows, err := fetchCopyTraderPerformance(client, favorites)
	if err != nil {
		return "Could not fetch copy traders: " + err.Error()
	}
	filtered := make([]CopyTraderPerf, 0, len(rows))
	for _, r := range rows {
		if r.IsFavorite || r.Running || r.Stats.TotalTrades > 0 || r.FetchErr != "" {
			filtered = append(filtered, r)
		}
	}
	rows = filtered
	if len(rows) == 0 {
		if lang == "zh" {
			return "未找到跟单机器人。"
		}
		return "No copy traders found."
	}

	title := fmt.Sprintf("📊 <b>Copy trader performance (%d)</b>", len(rows))
	if lang == "zh" {
		title = fmt.Sprintf("📊 <b>跟单表现 (%d)</b>", len(rows))
	}
	lines := []string{title, ""}
	for i, r := range rows {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, formatCopyTraderPerfLine(r, lang))
	}
	hint := "⭐ = favorite · /fav add NAME — mark favorite"
	if lang == "zh" {
		hint = "⭐ = 收藏 · /fav add 名称 — 添加收藏"
	}
	lines = append(lines, "", "<i>"+hint+"</i>")
	return strings.Join(lines, "\n")
}

func formatCopyTraderPerfLine(r CopyTraderPerf, lang string) string {
	prefix := "  "
	if r.IsFavorite {
		prefix = "⭐ "
	}
	layerTag := fmt.Sprintf("L%d", r.Layer)
	if r.Paused {
		layerTag += " PAUSED"
	}
	status := "stopped"
	if r.Running {
		status = "running"
	}
	head := fmt.Sprintf("%s<b>%s</b> · %s · %s", prefix, escapeHTML(r.Name), layerTag, status)

	if r.FetchErr != "" {
		return head + "\n⚠️ " + escapeHTML(r.FetchErr)
	}

	stats := r.Stats
	winPct := "—"
	if stats.TotalTrades > 0 {
		winPct = fmt.Sprintf("%.0f%%", stats.WinRate)
	}
	avgPnL := "—"
	if stats.TotalTrades > 0 {
		avgPnL = formatPnLMoney(stats.TotalPnL / float64(stats.TotalTrades))
	}
	streak := r.Streak
	if streak == "" {
		streak = "—"
	}

	if lang == "zh" {
		return head + fmt.Sprintf("\n盈亏 <b>%s</b> · %d胜/%d负 · %s · %d笔",
			formatPnLMoney(stats.TotalPnL), stats.WinTrades, stats.LossTrades, winPct, stats.TotalTrades) +
			fmt.Sprintf("\n均 %s/笔 · 连胜 %s", avgPnL, streak)
	}
	return head + fmt.Sprintf("\nPnL <b>%s</b> · %dW/%dL · %s · %d trades",
		formatPnLMoney(stats.TotalPnL), stats.WinTrades, stats.LossTrades, winPct, stats.TotalTrades) +
		fmt.Sprintf("\nAvg %s/tr · streak %s", avgPnL, streak)
}
