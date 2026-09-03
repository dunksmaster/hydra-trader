package telegram

import (
	"encoding/json"
	"fmt"
	"strings"
)

func formatCopyStatusReport(client *quickClient, lang string) string {
	body, err := client.get("/api/my-traders")
	if err != nil {
		return "Could not fetch traders: " + err.Error()
	}
	var copyTraders []map[string]any
	// my-traders returns a JSON array
	var arr []map[string]any
	if json.Unmarshal(body, &arr) == nil {
		for _, tr := range arr {
			if isCopyTraderRow(client, tr) {
				copyTraders = append(copyTraders, tr)
			}
		}
	} else {
		var out struct {
			Traders []map[string]any `json:"traders"`
		}
		if json.Unmarshal(body, &out) == nil {
			for _, tr := range out.Traders {
				if isCopyTraderRow(client, tr) {
					copyTraders = append(copyTraders, tr)
				}
			}
		} else {
			return "Could not parse traders"
		}
	}
	if len(copyTraders) == 0 {
		return "No copy trader found. Create \"Autopilot Copy\" first."
	}

	title := fmt.Sprintf("<b>Copy bots (%d)</b>", len(copyTraders))
	if lang == "sq" {
		title = fmt.Sprintf("<b>Copy botët (%d)</b>", len(copyTraders))
	}
	lines := []string{title}

	for _, copyTrader := range copyTraders {
		id := fmt.Sprint(copyTrader["trader_id"])
		if id == "" {
			id = fmt.Sprint(copyTrader["id"])
		}
		name := fmt.Sprint(copyTrader["trader_name"], copyTrader["name"])
		running := fmt.Sprint(copyTrader["is_running"]) == "true"
		strategyID := fmt.Sprint(copyTrader["strategy_id"])

		runningLabel := "stopped"
		if running {
			runningLabel = "running"
		}

		leader := "?"
		mode := "?"
		notional := "?"
		maxPos := "?"
		copyLayer := "L2"
		paused := false
		if strategyID != "" && strategyID != "<nil>" {
			if stBody, stErr := client.get("/api/strategies/" + strategyID); stErr == nil {
				var st map[string]any
				if json.Unmarshal(stBody, &st) == nil {
					if cfg, ok := st["config"].(map[string]any); ok {
						if copyCfg, ok := cfg["copy_config"].(map[string]any); ok {
							leader = fmt.Sprint(copyCfg["leader_address"])
							mode = fmt.Sprint(copyCfg["copy_mode"])
							notional = fmt.Sprint(copyCfg["notional_usd"])
							maxPos = fmt.Sprint(copyCfg["max_positions"])
							switch v := copyCfg["copy_layer"].(type) {
							case float64:
								copyLayer = fmt.Sprintf("L%.0f", v)
							case int:
								copyLayer = fmt.Sprintf("L%d", v)
							}
							if v, ok := copyCfg["copy_paused"].(bool); ok {
								paused = v
							}
						}
					}
				}
			}
		}

		layerLabel := copyLayer
		if paused {
			layerLabel += " PAUSED"
		}

		legCount := "?"
		if statusBody, stErr := client.get("/api/status?trader_id=" + id); stErr == nil {
			var status map[string]any
			if json.Unmarshal(statusBody, &status) == nil {
				if pos, ok := status["positions"].([]any); ok {
					legCount = fmt.Sprintf("%d", len(pos))
				} else if pc, ok := status["position_count"].(float64); ok {
					legCount = fmt.Sprintf("%.0f", pc)
				}
			}
		}

		lines = append(lines, fmt.Sprintf("• <b>%s</b> — %s | %s", escapeHTML(name), runningLabel, layerLabel))
		lines = append(lines, fmt.Sprintf("  Leader: %s", leader))
		lines = append(lines, fmt.Sprintf("  Mode: %s | $%s × %s leg(s) | open: %s", mode, notional, maxPos, legCount))
	}

	return strings.Join(lines, "\n")
}

func isCopyTraderRow(client *quickClient, tr map[string]any) bool {
	name := strings.ToLower(fmt.Sprint(tr["trader_name"], tr["name"]))
	if strings.Contains(name, "copy") {
		return true
	}
	strategyID := fmt.Sprint(tr["strategy_id"])
	if strategyID == "" || strategyID == "<nil>" {
		return false
	}
	stBody, err := client.get("/api/strategies/" + strategyID)
	if err != nil {
		return false
	}
	var st map[string]any
	if json.Unmarshal(stBody, &st) != nil {
		return false
	}
	cfg, _ := st["config"].(map[string]any)
	return fmt.Sprint(cfg["strategy_type"]) == "copy_trading"
}
