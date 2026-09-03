package telegram

import (
	"encoding/json"
	"fmt"
	"strings"

	"nofx/store"
	"nofx/telegram/agent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func handleStrategyCommand(bot *tgbotapi.BotAPI, chatID int64, cmd string, st *store.Store, botUserID string) {
	lang := st.TelegramConfig().GetLanguage()
	fields := strings.Fields(strings.TrimSpace(cmd))
	if len(fields) < 2 {
		profile := st.GetCopyStrategyProfile()
		msg := fmt.Sprintf("<b>Copy strategy profile</b>\n\nActive: <code>%s</code>\n\n/strategy current — restore five-leader snapshot\n/strategy layer1 — apply L1 priority layout", profile)
		if lang == "zh" {
			msg = fmt.Sprintf("<b>跟单策略配置</b>\n\n当前: <code>%s</code>\n\n/strategy current — 恢复五位 leader 快照\n/strategy layer1 — 启用 L1 优先布局", profile)
		}
		sendHTMLMsg(bot, chatID, msg)
		return
	}
	target := strings.ToLower(strings.TrimSpace(fields[1]))
	result, err := st.ApplyCopyStrategyProfile(botUserID, target)
	if err != nil {
		sendMsg(bot, chatID, "Strategy switch failed: "+err.Error())
		return
	}
	sendHTMLMsg(bot, chatID, "✅ "+escapeHTML(result))
}

func handleLeadersCommand(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, botUserID string, apiPort int) {
	lang := st.TelegramConfig().GetLanguage()
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	sendHTMLMsg(bot, chatID, formatLeadersReport(newQuickClient(apiPort, jwt), st, lang))
}

func formatLeadersReport(client *quickClient, st *store.Store, lang string) string {
	body, err := client.get("/api/my-traders")
	if err != nil {
		return "Could not fetch traders: " + err.Error()
	}
	var arr []map[string]any
	if json.Unmarshal(body, &arr) != nil {
		return "Could not parse traders"
	}

	profile := st.GetCopyStrategyProfile()
	title := fmt.Sprintf("<b>Copy leaders</b> (profile: <code>%s</code>)", profile)
	if lang == "zh" {
		title = fmt.Sprintf("<b>跟单 Leader</b> (配置: <code>%s</code>)", profile)
	}
	lines := []string{title, ""}

	type row struct {
		layer int
		line  string
	}
	var rows []row
	for _, tr := range arr {
		if !isCopyTraderRow(client, tr) {
			continue
		}
		name := fmt.Sprint(tr["trader_name"], tr["name"])
		strategyID := fmt.Sprint(tr["strategy_id"])
		leader := "?"
		layer := 2
		paused := false
		if strategyID != "" && strategyID != "<nil>" {
			if stBody, stErr := client.get("/api/strategies/" + strategyID); stErr == nil {
				var stMap map[string]any
				if json.Unmarshal(stBody, &stMap) == nil {
					if cfg, ok := stMap["config"].(map[string]any); ok {
						if copyCfg, ok := cfg["copy_config"].(map[string]any); ok {
							leader = fmt.Sprint(copyCfg["leader_address"])
							if v, ok := copyCfg["copy_layer"].(float64); ok {
								layer = int(v)
							}
							if v, ok := copyCfg["copy_paused"].(bool); ok {
								paused = v
							}
						}
					}
				}
			}
		}
		layerTag := fmt.Sprintf("L%d", layer)
		if paused {
			layerTag += " PAUSED"
		}
		running := "stopped"
		if fmt.Sprint(tr["is_running"]) == "true" {
			running = "running"
		}
		rows = append(rows, row{
			layer: layer,
			line:  fmt.Sprintf("• <b>%s</b> [%s] — %s\n  %s", escapeHTML(name), layerTag, running, leader),
		})
	}
	if len(rows) == 0 {
		return "No copy traders found."
	}
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].layer < rows[i].layer {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	for _, r := range rows {
		lines = append(lines, r.line)
	}
	return strings.Join(lines, "\n")
}
