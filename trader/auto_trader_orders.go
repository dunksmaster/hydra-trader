package trader

import (
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"strings"
	"time"
)

const (
	// marginOverheadFactor and takerFeeRate approximate the total funds an
	// exchange reserves when opening a position:
	// totalRequired ≈ positionSize/leverage + positionSize*takerFeeRate + positionSize/leverage*1%
	//              = positionSize * (marginOverheadFactor/leverage + takerFeeRate)
	marginOverheadFactor = 1.01
	takerFeeRate         = 0.001

	// positionSizeSafetyFactor leaves a buffer below the maximum affordable
	// position size so a price move between sizing and execution cannot
	// trigger an insufficient-margin rejection.
	positionSizeSafetyFactor = 0.98
)

func currentAccountEquity(balance map[string]interface{}, availableBalance float64) float64 {
	for _, key := range []string{"totalEquity", "total_equity"} {
		if equity, ok := balance[key].(float64); ok && equity > 0 {
			return equity
		}
	}
	if wallet, ok := balance["totalWalletBalance"].(float64); ok && wallet > 0 {
		return wallet
	}
	return availableBalance
}

// executeDecisionWithRecord executes AI decision and records detailed information
func (at *AutoTrader) executeDecisionWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	if decision != nil {
		if resolved := at.resolveLiveSymbol(decision.Symbol); resolved != "" && resolved != decision.Symbol {
			at.logInfof("Resolved decision symbol %s → %s", decision.Symbol, resolved)
			decision.Symbol = resolved
		}
		if err := at.blockAIOnOverflowLeg(decision.Symbol, decision.Action); err != nil {
			at.logInfof("🛡️ %v", err)
			return err
		}
	}
	switch decision.Action {
	case "open_long":
		return at.executeOpenLongWithRecord(decision, actionRecord)
	case "open_short":
		return at.executeOpenShortWithRecord(decision, actionRecord)
	case "close_long":
		return at.executeCloseLongWithRecord(decision, actionRecord)
	case "close_short":
		return at.executeCloseShortWithRecord(decision, actionRecord)
	case "hold":
		if at.usesSignalManagedExit() {
			if err := at.trader.CancelTakeProfitOrders(decision.Symbol); err != nil {
				logger.Infof("  ⚠ Failed to remove fixed take profit for signal-managed hold: %v", err)
			}
		}
		return nil
	case "wait":
		return nil
	default:
		return fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// executeOpenLongWithRecord executes open long position and records detailed information
func (at *AutoTrader) executeOpenLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  📈 Open long: %s", decision.Symbol)

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(at.effectivePositionCountForRisk(positions)); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
			return fmt.Errorf("❌ %s already has long position, close it first", decision.Symbol)
		}
	}

	// Get current price and reject invalid protection before opening exposure.
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return fmt.Errorf("failed to get market data for %s: %w", decision.Symbol, err)
	}
	if err := validateProtectionPrices(decision.Action, marketData.CurrentPrice, decision.StopLoss, decision.TakeProfit, at.usesSignalManagedExit()); err != nil {
		return err
	}

	// Get balance (needed for multiple checks)
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Get equity for position value ratio check
	equity := currentAccountEquity(balance, availableBalance)

	at.applyAutopilotFullSizeOpen(decision, equity)

	// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
	adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
	if wasCapped {
		decision.PositionSizeUSD = adjustedPositionSize
	}

	// ⚠️ Auto-adjust position size if insufficient margin
	marginFactor := marginOverheadFactor/float64(decision.Leverage) + takerFeeRate
	maxAffordablePositionSize := availableBalance / marginFactor

	actualPositionSize := decision.PositionSizeUSD
	if actualPositionSize > maxAffordablePositionSize {
		adjustedSize := maxAffordablePositionSize * positionSizeSafetyFactor
		logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			actualPositionSize, maxAffordablePositionSize, adjustedSize)
		actualPositionSize = adjustedSize
		decision.PositionSizeUSD = actualPositionSize
	}

	// [CODE ENFORCED] Minimum position size check
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	order, err := at.trader.OpenLong(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		return fmt.Errorf("failed to open long position for %s: %w", decision.Symbol, err)
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "open_long", quantity, marketData.CurrentPrice, decision.Leverage, 0)

	// Record position opening time
	posKey := decision.Symbol + "_long"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	at.applyDefaultProtectivePrices(decision, marketData.CurrentPrice)
	if err := at.placeProtectiveOrders(decision.Symbol, "LONG", quantity, decision.StopLoss, decision.TakeProfit); err != nil {
		at.logErrorf("🛡️ Protective orders failed after opening %s long: %v — position is live, protectNakedPositions will retry", decision.Symbol, err)
	}

	return nil
}

// executeOpenShortWithRecord executes open short position and records detailed information
func (at *AutoTrader) executeOpenShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  📉 Open short: %s", decision.Symbol)

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(at.effectivePositionCountForRisk(positions)); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
			return fmt.Errorf("❌ %s already has short position, close it first", decision.Symbol)
		}
	}

	// Get current price and reject invalid protection before opening exposure.
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return fmt.Errorf("failed to get market data for %s: %w", decision.Symbol, err)
	}
	if err := validateProtectionPrices(decision.Action, marketData.CurrentPrice, decision.StopLoss, decision.TakeProfit, at.usesSignalManagedExit()); err != nil {
		return err
	}

	// Get balance (needed for multiple checks)
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Get equity for position value ratio check
	equity := currentAccountEquity(balance, availableBalance)

	at.applyAutopilotFullSizeOpen(decision, equity)

	// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
	adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
	if wasCapped {
		decision.PositionSizeUSD = adjustedPositionSize
	}

	// ⚠️ Auto-adjust position size if insufficient margin
	marginFactor := marginOverheadFactor/float64(decision.Leverage) + takerFeeRate
	maxAffordablePositionSize := availableBalance / marginFactor

	actualPositionSize := decision.PositionSizeUSD
	if actualPositionSize > maxAffordablePositionSize {
		adjustedSize := maxAffordablePositionSize * positionSizeSafetyFactor
		logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			actualPositionSize, maxAffordablePositionSize, adjustedSize)
		actualPositionSize = adjustedSize
		decision.PositionSizeUSD = actualPositionSize
	}

	// [CODE ENFORCED] Minimum position size check
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	order, err := at.trader.OpenShort(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		return fmt.Errorf("failed to open short position for %s: %w", decision.Symbol, err)
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "open_short", quantity, marketData.CurrentPrice, decision.Leverage, 0)

	// Record position opening time
	posKey := decision.Symbol + "_short"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	at.applyDefaultProtectivePrices(decision, marketData.CurrentPrice)
	if err := at.placeProtectiveOrders(decision.Symbol, "SHORT", quantity, decision.StopLoss, decision.TakeProfit); err != nil {
		at.logErrorf("🛡️ Protective orders failed after opening %s short: %v — position is live, protectNakedPositions will retry", decision.Symbol, err)
	}

	return nil
}

// executeCloseLongWithRecord executes close long position and records detailed information
func (at *AutoTrader) executeCloseLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close long: %s", decision.Symbol)

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return fmt.Errorf("failed to get market data for %s: %w", decision.Symbol, err)
	}
	actionRecord.Price = marketData.CurrentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.Normalize(decision.Symbol)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// Overflow closes must use live exchange qty — local DB may hold legacy full-mirror size.
	if !at.isOverflowExec() && at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "LONG"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok && amt > 0 {
						quantity = amt
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Close position (0 = close all; decision.Quantity = partial close)
	closeQty := 0.0
	if decision.Quantity > 0 {
		closeQty = decision.Quantity
		if closeQty > quantity {
			closeQty = quantity
		}
	}
	order, err := at.trader.CloseLong(decision.Symbol, closeQty)
	if err != nil {
		return fmt.Errorf("failed to close long position for %s: %w", decision.Symbol, err)
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	confirmedQty := quantity
	if closeQty > 0 {
		confirmedQty = closeQty
	}
	at.recordAndConfirmOrder(order, decision.Symbol, "close_long", confirmedQty, marketData.CurrentPrice, 0, entryPrice)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// executeCloseShortWithRecord executes close short position and records detailed information
func (at *AutoTrader) executeCloseShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close short: %s", decision.Symbol)

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return fmt.Errorf("failed to get market data for %s: %w", decision.Symbol, err)
	}
	actionRecord.Price = marketData.CurrentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.Normalize(decision.Symbol)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	if !at.isOverflowExec() && at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "SHORT"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok {
						quantity = -amt // positionAmt is negative for short
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Close position (0 = close all; decision.Quantity = partial close)
	closeQty := 0.0
	if decision.Quantity > 0 {
		closeQty = decision.Quantity
		if closeQty > quantity {
			closeQty = quantity
		}
	}
	order, err := at.trader.CloseShort(decision.Symbol, closeQty)
	if err != nil {
		return fmt.Errorf("failed to close short position for %s: %w", decision.Symbol, err)
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	confirmedQty := quantity
	if closeQty > 0 {
		confirmedQty = closeQty
	}
	at.recordAndConfirmOrder(order, decision.Symbol, "close_short", confirmedQty, marketData.CurrentPrice, 0, entryPrice)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

const (
	defaultOpenStopPct = 2.0
	defaultOpenTakePct = 6.0
)

func applyDefaultProtectivePrices(decision *kernel.Decision, entry float64) {
	if decision == nil || entry <= 0 {
		return
	}
	long := decision.Action != "open_short"
	if decision.StopLoss <= 0 {
		if long {
			decision.StopLoss = entry * (1 - defaultOpenStopPct/100)
		} else {
			decision.StopLoss = entry * (1 + defaultOpenStopPct/100)
		}
		logger.Errorf("🛡️ %s had stop_loss=0; using %.1f%% default at %.6f", decision.Symbol, defaultOpenStopPct, decision.StopLoss)
	}
	if decision.TakeProfit <= 0 {
		if long {
			decision.TakeProfit = entry * (1 + defaultOpenTakePct/100)
		} else {
			decision.TakeProfit = entry * (1 - defaultOpenTakePct/100)
		}
		logger.Errorf("🛡️ %s had take_profit=0; using %.1f%% default at %.6f", decision.Symbol, defaultOpenTakePct, decision.TakeProfit)
	}
}

func (at *AutoTrader) applyDefaultProtectivePrices(decision *kernel.Decision, entry float64) {
	applyDefaultProtectivePrices(decision, entry)
}

type combinedTPSLTrader interface {
	SetTPSL(symbol, positionSide string, quantity, stopLoss, takeProfit float64) error
}

func (at *AutoTrader) placeProtectiveOrders(symbol, side string, quantity, stopLoss, takeProfit float64) error {
	if stopLoss <= 0 {
		return fmt.Errorf("refusing to send stop_loss=0 to the exchange")
	}
	if combined, ok := at.trader.(combinedTPSLTrader); ok {
		if err := combined.SetTPSL(symbol, side, quantity, stopLoss, takeProfit); err != nil {
			return fmt.Errorf("stop loss: %w", err)
		}
	} else {
		if err := at.trader.SetStopLoss(symbol, side, quantity, stopLoss); err != nil {
			return fmt.Errorf("stop loss: %w", err)
		}
		if takeProfit > 0 {
			if err := at.trader.SetTakeProfit(symbol, side, quantity, takeProfit); err != nil {
				at.logErrorf("🛡️ Failed to set take profit on %s: %v", symbol, err)
			}
		}
	}
	if at.fallbackStopsPlaced == nil {
		at.fallbackStopsPlaced = make(map[string]bool)
	}
	at.fallbackStopsPlaced[symbol+"_"+strings.ToLower(side)] = true
	return nil
}

func (at *AutoTrader) protectNakedPositions(ctx *kernel.Context) {
	if at == nil || ctx == nil || at.trader == nil {
		return
	}
	if at.fallbackStopsPlaced == nil {
		at.fallbackStopsPlaced = make(map[string]bool)
	}
	for _, pos := range ctx.Positions {
		if pos.Quantity <= 0 {
			continue
		}
		side := strings.ToUpper(pos.Side)
		if side != "LONG" && side != "SHORT" {
			continue
		}
		key := pos.Symbol + "_" + strings.ToLower(side)
		if at.fallbackStopsPlaced[key] {
			continue
		}
		entry := pos.MarkPrice
		if entry <= 0 {
			entry = pos.EntryPrice
		}
		if entry <= 0 {
			continue
		}
		d := &kernel.Decision{Symbol: pos.Symbol, Action: "open_long"}
		if side == "SHORT" {
			d.Action = "open_short"
		}
		applyDefaultProtectivePrices(d, entry)
		if err := at.placeProtectiveOrders(pos.Symbol, side, pos.Quantity, d.StopLoss, d.TakeProfit); err != nil {
			at.logErrorf("🛡️ Failed to attach fallback stops to open %s %s: %v — set stops manually on the exchange", pos.Symbol, side, err)
			continue
		}
		at.logErrorf("🛡️ Attached fallback %.1f%% stop / %.1f%% take-profit to already-open %s %s (AI had left it unprotected)", defaultOpenStopPct, defaultOpenTakePct, pos.Symbol, side)
	}
}
