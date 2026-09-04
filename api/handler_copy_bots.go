package api

import (
	"net/http"
	"strings"

	"nofx/store"

	"github.com/gin-gonic/gin"
)

type copyBotPosition struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	EntryPrice    float64 `json:"entry_price"`
	MarkPrice     float64 `json:"mark_price"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	Leverage      float64 `json:"leverage"`
}

type copyBotStats struct {
	TradeCount   int     `json:"trade_count"`
	WinRate      float64 `json:"win_rate"`
	ProfitFactor float64 `json:"profit_factor"`
	TotalPnL     float64 `json:"total_pnl"`
	TotalPnLPct  float64 `json:"total_pnl_pct"`
}

type copyBotRow struct {
	TraderID     string                    `json:"trader_id"`
	TraderName   string                    `json:"trader_name"`
	Exchange     string                    `json:"exchange"`
	ExchangeID   string                    `json:"exchange_id"`
	IsRunning    bool                      `json:"is_running"`
	StrategyID   string                    `json:"strategy_id"`
	StrategyName string                    `json:"strategy_name"`
	StrategyType string                    `json:"strategy_type"`
	CopyConfig   *store.CopyStrategyConfig `json:"copy_config,omitempty"`
	Account      map[string]interface{}    `json:"account,omitempty"`
	Positions    []copyBotPosition         `json:"positions"`
	Stats        *copyBotStats             `json:"stats,omitempty"`
	LastDecision string                    `json:"last_decision,omitempty"`
}

// handleCopyBots returns an aggregated view of all copy-trading bots for the user.
func (s *Server) handleCopyBots(c *gin.Context) {
	userID := c.GetString("user_id")
	traders, err := s.store.Trader().List(userID)
	if err != nil {
		SafeInternalError(c, "Failed to get trader list", err)
		return
	}

	profile := s.store.GetCopyStrategyProfile()
	rows := make([]copyBotRow, 0)
	var walletSummary map[string]interface{}
	walletSlots := 5
	openLegs := 0

	for _, tr := range traders {
		if tr.StrategyID == "" {
			continue
		}
		strategy, err := s.store.Strategy().Get(userID, tr.StrategyID)
		if err != nil || strategy == nil {
			continue
		}
		cfg, err := strategy.ParseConfig()
		if err != nil || cfg.StrategyType != "copy_trading" || cfg.CopyConfig == nil {
			continue
		}
		copyCfg := cfg.CopyConfig
		copyCfg.Normalize()

		exchangeType := ""
		if fullCfg, cfgErr := s.store.Trader().GetFullConfig(userID, tr.ID); cfgErr == nil && fullCfg.Exchange != nil {
			exchangeType = fullCfg.Exchange.ExchangeType
		}

		isRunning := tr.IsRunning
		var account map[string]interface{}
		var positions []copyBotPosition
		var stats *copyBotStats
		lastDecision := ""

		if at, err := s.traderManager.GetTrader(tr.ID); err == nil {
			if status := at.GetStatus(); status != nil {
				if running, ok := status["is_running"].(bool); ok {
					isRunning = running
				}
			}
			if acct, acctErr := at.GetAccountInfo(); acctErr == nil {
				account = acct
			}
			if posList, posErr := at.GetPositions(); posErr == nil {
				positions = mapCopyBotPositions(posList)
				if walletSummary == nil && strings.EqualFold(exchangeType, "hyperliquid") {
					walletSummary = account
					if copyCfg.WalletCopySlots > 0 {
						walletSlots = copyCfg.WalletCopySlots
					}
					openLegs = countOpenLegs(posList)
				}
			}
			if st := at.GetStore(); st != nil {
				if fullStats, statsErr := st.Position().GetFullStatsByTraderFilters(
					[]string{at.GetID()}, nil, at.GetInitialBalance(),
				); statsErr == nil && fullStats != nil {
					stats = &copyBotStats{
						TradeCount:   fullStats.TotalTrades,
						WinRate:      fullStats.WinRate,
						ProfitFactor: fullStats.ProfitFactor,
						TotalPnL:     fullStats.TotalPnL,
					}
					if at.GetInitialBalance() > 0 {
						stats.TotalPnLPct = fullStats.TotalPnL / at.GetInitialBalance() * 100
					}
				}
				if records, decErr := st.Decision().GetLatestRecords(at.GetID(), 3); decErr == nil && len(records) > 0 {
					last := records[len(records)-1]
					if len(last.Decisions) > 0 {
						lastDecision = strings.TrimSpace(last.Decisions[len(last.Decisions)-1].Reasoning)
					} else if last.ErrorMessage != "" {
						lastDecision = last.ErrorMessage
					}
				}
			}
		}

		rows = append(rows, copyBotRow{
			TraderID:     tr.ID,
			TraderName:   tr.Name,
			Exchange:     exchangeType,
			ExchangeID:   tr.ExchangeID,
			IsRunning:    isRunning,
			StrategyID:   strategy.ID,
			StrategyName: strategy.Name,
			StrategyType: cfg.StrategyType,
			CopyConfig:   copyCfg,
			Account:      account,
			Positions:    positions,
			Stats:        stats,
			LastDecision: lastDecision,
		})
	}

	liveCount := 0
	pausedCount := 0
	for _, row := range rows {
		if row.CopyConfig != nil && (row.CopyConfig.CopyPaused || row.CopyConfig.CopyLayer >= 3) {
			pausedCount++
		} else if row.IsRunning {
			liveCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"profile": profile,
		"wallet": gin.H{
			"equity":          walletFieldFloat(walletSummary, "total_equity"),
			"available":       walletFieldFloat(walletSummary, "available_balance"),
			"unrealized_pnl":  walletFieldFloat(walletSummary, "unrealized_profit"),
			"total_pnl":       walletFieldFloat(walletSummary, "total_pnl"),
			"open_legs":       openLegs,
			"wallet_slots":    walletSlots,
			"margin_used_pct": walletFieldFloat(walletSummary, "margin_used_pct"),
		},
		"summary": gin.H{
			"copy_bot_count": len(rows),
			"live_count":     liveCount,
			"paused_count":   pausedCount,
		},
		"bots": rows,
	})
}

func mapCopyBotPositions(posList []map[string]interface{}) []copyBotPosition {
	out := make([]copyBotPosition, 0, len(posList))
	for _, pos := range posList {
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		qty := posFloat(pos, "positionAmt", "position_amt", "quantity")
		if qty == 0 && symbol == "" {
			continue
		}
		if qty < 0 {
			qty = -qty
		}
		out = append(out, copyBotPosition{
			Symbol:        symbol,
			Side:          side,
			Quantity:      qty,
			EntryPrice:    posFloat(pos, "entryPrice", "entry_price"),
			MarkPrice:     posFloat(pos, "markPrice", "mark_price"),
			UnrealizedPnL: posFloat(pos, "unRealizedProfit", "unrealized_pnl", "unrealizedProfit"),
			Leverage:      posFloat(pos, "leverage"),
		})
	}
	return out
}

func countOpenLegs(posList []map[string]interface{}) int {
	n := 0
	for _, pos := range posList {
		if posFloat(pos, "positionAmt", "position_amt", "quantity") != 0 {
			n++
		}
	}
	return n
}

func posFloat(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if v, ok := m[key].(float64); ok {
			return v
		}
	}
	return 0
}

func walletFieldFloat(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func strategyMetaForTrader(s *Server, userID string, strategyID string) (strategyType string, copyLayer int, copyPaused bool) {
	if strategyID == "" {
		return "", 0, false
	}
	strategy, err := s.store.Strategy().Get(userID, strategyID)
	if err != nil || strategy == nil {
		return "", 0, false
	}
	cfg, err := strategy.ParseConfig()
	if err != nil {
		return "", 0, false
	}
	strategyType = cfg.StrategyType
	if cfg.CopyConfig != nil {
		copyLayer = cfg.CopyConfig.CopyLayer
		copyPaused = cfg.CopyConfig.CopyPaused
	}
	return strategyType, copyLayer, copyPaused
}

// enrichTraderListRow adds strategy_type and copy_layer to my-traders entries.
func enrichTraderListRow(s *Server, userID string, traderID, strategyID string, row map[string]interface{}) {
	st, layer, paused := strategyMetaForTrader(s, userID, strategyID)
	if st != "" {
		row["strategy_type"] = st
	}
	if st == "copy_trading" {
		row["copy_layer"] = layer
		row["copy_paused"] = paused
	}
}
