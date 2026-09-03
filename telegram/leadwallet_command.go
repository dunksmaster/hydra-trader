package telegram

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"nofx/store"
	"nofx/telegram/agent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func handleLeadWalletCommand(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, botUserID string, apiPort int) {
	lang := st.TelegramConfig().GetLanguage()
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	sendHTMLMsg(bot, chatID, formatLeadWalletReport(newQuickClient(apiPort, jwt), lang))
}

func formatLeadWalletReport(client *quickClient, lang string) string {
	copyTraders, err := fetchCopyTraders(client)
	if err != nil {
		return "Could not fetch copy traders: " + err.Error()
	}
	if len(copyTraders) == 0 {
		if lang == "zh" {
			return "未找到跟单机器人。"
		}
		return "No copy traders found."
	}

	type row struct {
		layer  int
		paused bool
		line   string
	}
	rows := make([]row, 0, len(copyTraders))
	for _, tr := range copyTraders {
		name := copyTraderDisplayName(strings.TrimSpace(fmt.Sprint(tr["trader_name"], tr["name"])))
		strategyID := fmt.Sprint(tr["strategy_id"])
		leader := ""
		layer := 2
		paused := false
		if strategyID != "" && strategyID != "<nil>" {
			if stBody, stErr := client.get("/api/strategies/" + strategyID); stErr == nil {
				var stMap map[string]any
				if json.Unmarshal(stBody, &stMap) == nil {
					if cfg, ok := stMap["config"].(map[string]any); ok {
						if copyCfg, ok := cfg["copy_config"].(map[string]any); ok {
							leader = strings.TrimSpace(fmt.Sprint(copyCfg["leader_address"]))
							layer = copyLayerFromConfig(copyCfg)
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
		addr := shortLeaderAddr(leader)
		if addr == "" {
			addr = "?"
		}
		rows = append(rows, row{
			layer:  layer,
			paused: paused,
			line:   fmt.Sprintf("• <b>%s</b> [%s]\n  <code>%s</code>", escapeHTML(name), layerTag, escapeHTML(addr)),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].layer != rows[j].layer {
			return rows[i].layer < rows[j].layer
		}
		if rows[i].paused != rows[j].paused {
			return !rows[i].paused
		}
		return rows[i].line < rows[j].line
	})

	title := fmt.Sprintf("<b>Lead wallets (%d copy bots)</b>", len(rows))
	if lang == "zh" {
		title = fmt.Sprintf("<b>Leader 钱包 (%d 个跟单)</b>", len(rows))
	}
	lines := []string{title, ""}
	for _, r := range rows {
		lines = append(lines, r.line)
	}
	return strings.Join(lines, "\n")
}

func fetchCopyTraders(client *quickClient) ([]map[string]any, error) {
	body, err := client.get("/api/my-traders")
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if json.Unmarshal(body, &arr) == nil {
		var out []map[string]any
		for _, tr := range arr {
			if isCopyTraderRow(client, tr) {
				out = append(out, tr)
			}
		}
		return out, nil
	}
	var wrapped struct {
		Traders []map[string]any `json:"traders"`
	}
	if json.Unmarshal(body, &wrapped) != nil {
		return nil, fmt.Errorf("could not parse traders")
	}
	var out []map[string]any
	for _, tr := range wrapped.Traders {
		if isCopyTraderRow(client, tr) {
			out = append(out, tr)
		}
	}
	return out, nil
}

func copyLayerFromConfig(copyCfg map[string]any) int {
	switch v := copyCfg["copy_layer"].(type) {
	case float64:
		if v >= 1 && v <= 3 {
			return int(v)
		}
	case int:
		if v >= 1 && v <= 3 {
			return v
		}
	}
	return 2
}

func shortLeaderAddr(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" || addr == "<nil>" {
		return ""
	}
	if len(addr) <= 14 {
		return addr
	}
	return addr[:6] + "..." + addr[len(addr)-4:]
}
