package trader

import (
	"sync"

	"nofx/store"
)

var (
	copySiblingLookupMu sync.RWMutex
	copySiblingLookup   func(string) []*AutoTrader
)

// SetCopySiblingLookup registers how copy bots find siblings on the same exchange account.
func SetCopySiblingLookup(fn func(exchangeID string) []*AutoTrader) {
	copySiblingLookupMu.Lock()
	copySiblingLookup = fn
	copySiblingLookupMu.Unlock()
}

func lookupCopySiblings(exchangeID string) []*AutoTrader {
	copySiblingLookupMu.RLock()
	fn := copySiblingLookup
	copySiblingLookupMu.RUnlock()
	if fn == nil || exchangeID == "" {
		return nil
	}
	return fn(exchangeID)
}

func (at *AutoTrader) copySiblings() []*AutoTrader {
	if at == nil {
		return nil
	}
	return lookupCopySiblings(at.exchangeID)
}

func (at *AutoTrader) copyLayer() int {
	if at == nil || at.config.StrategyConfig == nil || at.config.StrategyConfig.CopyConfig == nil {
		return 2
	}
	layer := at.config.StrategyConfig.CopyConfig.CopyLayer
	if layer <= 0 {
		return 2
	}
	return layer
}

func (at *AutoTrader) copyPaused() bool {
	if at == nil || at.config.StrategyConfig == nil || at.config.StrategyConfig.CopyConfig == nil {
		return false
	}
	return at.config.StrategyConfig.CopyConfig.CopyPaused
}

func (at *AutoTrader) copyWalletSlots() int {
	cfg := at.config.StrategyConfig.CopyConfig
	if cfg == nil {
		return 2
	}
	slots := cfg.WalletCopySlots
	if slots <= 0 {
		slots = cfg.MaxPositions
	}
	if slots <= 0 {
		slots = 2
	}
	return slots
}

// countAllWalletLegs counts every open leg on the shared exchange wallet.
func countAllWalletLegs(positions []map[string]interface{}) int {
	n := 0
	for _, pos := range positions {
		amt := floatFromPos(pos, "positionAmt", "position_amt")
		if amt == 0 {
			continue
		}
		n++
	}
	return n
}

func siblingCopyLayer(at *AutoTrader) int {
	if at == nil {
		return 0
	}
	return at.copyLayer()
}

func siblingCopyConfig(at *AutoTrader) *store.CopyStrategyConfig {
	if at == nil || at.config.StrategyConfig == nil {
		return nil
	}
	return at.config.StrategyConfig.CopyConfig
}
