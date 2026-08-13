package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"nofx/mcp/payment"
	"nofx/store"
	"nofx/wallet"

	"github.com/gin-gonic/gin"
)

// resolveTraderIDForRequest returns trader_id from query or the user's running trader.
func (s *Server) resolveTraderIDForRequest(c *gin.Context) (string, error) {
	userID := c.GetString("user_id")
	if userID == "" {
		return "", fmt.Errorf("unauthorized")
	}

	if tid := strings.TrimSpace(c.Query("trader_id")); tid != "" {
		if _, err := s.store.Trader().GetFullConfig(userID, tid); err != nil {
			return "", fmt.Errorf("trader not found")
		}
		return tid, nil
	}

	return s.resolveRunningTraderID(userID)
}

func (s *Server) resolveRunningTraderID(userID string) (string, error) {
	traders, err := s.store.Trader().List(userID)
	if err != nil {
		return "", err
	}
	if len(traders) == 0 {
		return "", fmt.Errorf("no traders configured")
	}
	for _, t := range traders {
		if t.IsRunning {
			return t.ID, nil
		}
	}
	return traders[0].ID, nil
}

func (s *Server) buildChargeContext(userID, traderID, source string) context.Context {
	if traderID == "" {
		return context.Background()
	}
	model := "claw402"
	provider := "claw402"
	if cfg, cfgErr := s.store.Trader().GetFullConfig(userID, traderID); cfgErr == nil && cfg.AIModel != nil {
		provider = cfg.AIModel.Provider
		if cfg.AIModel.CustomModelName != "" {
			model = cfg.AIModel.CustomModelName
		} else {
			model = provider
		}
	}
	return payment.WithChargeContext(context.Background(), payment.ChargeContext{
		TraderID: traderID,
		Source:   source,
		Model:    model,
		Provider: provider,
	})
}

func (s *Server) chargeContextForTrader(c *gin.Context, source string) context.Context {
	traderID, err := s.resolveTraderIDForRequest(c)
	if err != nil || traderID == "" {
		return context.Background()
	}
	return s.buildChargeContext(c.GetString("user_id"), traderID, source)
}

// handleGetAICosts returns AI charges for a specific trader
func (s *Server) handleGetAICosts(c *gin.Context) {
	traderID := c.Query("trader_id")
	period := c.DefaultQuery("period", "today")

	if traderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "trader_id is required"})
		return
	}

	userID := c.GetString("user_id")
	if _, err := s.store.Trader().GetFullConfig(userID, traderID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader does not exist"})
		return
	}

	charges, total, err := s.store.AICharge().GetCharges(traderID, period)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"charges": charges,
		"total":   total,
		"count":   len(charges),
	})
}

// handleGetAICostsSummary returns AI cost summary across all traders
func (s *Server) handleGetAICostsSummary(c *gin.Context) {
	period := c.DefaultQuery("period", "today")

	total, count, byModel := s.store.AICharge().GetSummary(period)

	c.JSON(http.StatusOK, gin.H{
		"total":    total,
		"count":    count,
		"by_model": byModel,
	})
}

// handleGetAICostsDashboard returns aggregated spend for the dashboard strip.
func (s *Server) handleGetAICostsDashboard(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID, err := s.resolveTraderIDForRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	spend, err := s.store.AICharge().GetDashboardSpend(traderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	estimatedDaily := spend.SpentToday
	if spend.SpentWeek > 0 {
		weekDaily := spend.SpentWeek / 7
		if weekDaily > estimatedDaily {
			estimatedDaily = weekDaily
		}
	}

	fullCfg, cfgErr := s.store.Trader().GetFullConfig(userID, traderID)
	if estimatedDaily <= 0 && cfgErr == nil && fullCfg.AIModel != nil {
		modelName := fullCfg.AIModel.CustomModelName
		if modelName == "" {
			modelName = fullCfg.AIModel.Provider
		}
		scanMin := fullCfg.Trader.ScanIntervalMinutes
		if scanMin <= 0 {
			scanMin = 15
		}
		estimatedDaily, _ = store.EstimateRunway(0, modelName, scanMin)
	}

	projected7d := estimatedDaily * 7

	walletBalance := 0.0
	runwayDays := 0.0
	if at, memErr := s.traderManager.GetTrader(traderID); memErr == nil {
		if status, ok := at.GetStatus()["ai_wallet_balance_usdc"].(float64); ok {
			walletBalance = status
		}
	}
	if walletBalance <= 0 && cfgErr == nil && fullCfg.AIModel != nil && store.IsClaw402Config(fullCfg.AIModel.Provider) {
		if addr, addrErr := walletAddressFromPrivateKey(fullCfg.AIModel.APIKey.String()); addrErr == nil {
			if bal, balErr := wallet.QueryUSDCBalanceCached(addr); balErr == nil {
				walletBalance = bal
			}
		}
	}
	if estimatedDaily > 0 && walletBalance > 0 {
		runwayDays = walletBalance / estimatedDaily
	}

	c.JSON(http.StatusOK, gin.H{
		"spent_today":         spend.SpentToday,
		"spent_week":          spend.SpentWeek,
		"estimated_daily":     estimatedDaily,
		"projected_7d":        projected7d,
		"wallet_balance_usdc": walletBalance,
		"runway_days":         runwayDays,
		"call_count_today":    spend.CallCountToday,
		"call_count_week":     spend.CallCountWeek,
		"by_source":           spend.BySource,
	})
}
