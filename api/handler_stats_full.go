package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleStatisticsFull returns the full set of computed performance metrics for
// a single trader: win rate, profit factor, Sharpe ratio, max drawdown, and the
// average win/loss amounts. These are derived from the trader's CLOSED positions
// via store.Position().GetFullStatsByTraderFilters — the same computation the
// strategy engine feeds to the AI, so the dashboard and the model see identical
// numbers.
//
// The existing GET /statistics endpoint only returns cycle/position counts; this
// endpoint exposes the richer trade-quality metrics the terminal dashboard needs.
func (s *Server) handleStatisticsFull(c *gin.Context) {
	userID := c.GetString("user_id")
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	traderIDs, traderIDPatterns, initialBalance, store, ok := s.positionHistoryScope(userID, traderID)
	if !ok || store == nil {
		SafeNotFound(c, "Trader")
		return
	}

	stats, err := store.Position().GetFullStatsByTraderFilters(traderIDs, traderIDPatterns, initialBalance)
	if err != nil {
		SafeInternalError(c, "Get full statistics", err)
		return
	}

	c.JSON(http.StatusOK, stats)
}
