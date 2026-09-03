package bitget

import (
	"encoding/json"
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/provider/hyperliquid"
	"strings"
	"time"
)

const tradableCatalogTTL = 10 * time.Minute

type contractCatalog struct {
	bySymbol map[string]contractEntry
	byBase   map[string]string // NVDA -> NVDAUSDT
}

type contractEntry struct {
	Symbol string
	Base   string
	IsRWA  bool
}

// NativeUSDTPerpSymbol maps HL-style xyz:NVDA / NVDAUSDT to Bitget USDT perp ticker guess.
func NativeUSDTPerpSymbol(symbol string) string {
	return bitgetNativeSymbol(symbol)
}

func hyperliquidBaseKey(symbol string) string {
	return hyperliquid.NormalizeXYZAlias(symbol)
}

// ResolveHyperliquidSymbol maps a Hyperliquid board symbol onto a live Bitget USDT-FUTURES ticker.
func (t *BitgetTrader) ResolveHyperliquidSymbol(symbol string) (string, bool) {
	catalog, err := t.loadContractCatalog()
	if err != nil {
		return "", false
	}
	return catalog.resolve(symbol)
}

func (c *contractCatalog) resolve(symbol string) (string, bool) {
	if c == nil {
		return "", false
	}

	native := NativeUSDTPerpSymbol(symbol)
	if native != "" {
		if entry, ok := c.bySymbol[native]; ok {
			return entry.Symbol, true
		}
	}

	for _, base := range uniqueBases(symbol) {
		if mapped, ok := c.byBase[base]; ok {
			return mapped, true
		}
	}
	return "", false
}

func uniqueBases(symbol string) []string {
	seen := map[string]bool{}
	var out []string
	for _, base := range []string{
		hyperliquidBaseKey(symbol),
		strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(symbol)), "XYZ:"),
		strings.TrimSuffix(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(symbol)), "XYZ:"), "USDT"),
	} {
		base = strings.TrimSpace(base)
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		out = append(out, base)
	}
	return out
}

// HasTradableSymbol reports whether symbol exists on Bitget USDT-FUTURES.
func (t *BitgetTrader) HasTradableSymbol(symbol string) bool {
	_, ok := t.ResolveHyperliquidSymbol(symbol)
	return ok
}

// FilterCandidateCoins keeps HL-ranked symbols that Bitget lists (via symbol or baseCoin) and rewrites to native USDT perps.
func (t *BitgetTrader) FilterCandidateCoins(coins []kernel.CandidateCoin) []kernel.CandidateCoin {
	catalog, err := t.loadContractCatalog()
	if err != nil {
		logger.Warnf("⚠️ Bitget: could not load contract catalog: %v (keeping unfiltered board)", err)
		return coins
	}

	out := make([]kernel.CandidateCoin, 0, len(coins))
	seen := map[string]bool{}
	for _, coin := range coins {
		native, ok := catalog.resolve(coin.Symbol)
		if !ok {
			logger.Infof("⏭️ Bitget: skip %s (no USDT-FUTURES match by symbol/baseCoin)", coin.Symbol)
			continue
		}
		if seen[native] {
			continue
		}
		seen[native] = true
		coin.Symbol = native
		out = append(out, coin)
	}
	logger.Infof("✅ Bitget: %d/%d hyper_rank candidates matched Bitget baseCoin/USDT list", len(out), len(coins))
	return out
}

// ListTradableRWABases returns Bitget RWA stock bases sorted alphabetically.
func (t *BitgetTrader) ListTradableRWABases() ([]string, error) {
	catalog, err := t.loadContractCatalog()
	if err != nil {
		return nil, err
	}
	bases := make([]string, 0)
	for base, sym := range catalog.byBase {
		if entry, ok := catalog.bySymbol[sym]; ok && entry.IsRWA {
			bases = append(bases, base)
		}
	}
	return bases, nil
}

func (t *BitgetTrader) loadContractCatalog() (*contractCatalog, error) {
	t.tradableCatalogMutex.RLock()
	if t.contractCatalog != nil && time.Since(t.tradableCatalogTime) < tradableCatalogTTL {
		catalog := t.contractCatalog
		t.tradableCatalogMutex.RUnlock()
		return catalog, nil
	}
	t.tradableCatalogMutex.RUnlock()

	t.tradableCatalogMutex.Lock()
	defer t.tradableCatalogMutex.Unlock()

	if t.contractCatalog != nil && time.Since(t.tradableCatalogTime) < tradableCatalogTTL {
		return t.contractCatalog, nil
	}

	params := map[string]interface{}{
		"productType": utaFuturesCategory,
	}
	data, err := t.doRequest("GET", bitgetContractsPath, params)
	if err != nil {
		return nil, err
	}

	var contracts []struct {
		Symbol       string `json:"symbol"`
		BaseCoin     string `json:"baseCoin"`
		QuoteCoin    string `json:"quoteCoin"`
		SymbolStatus string `json:"symbolStatus"`
		IsRWA        string `json:"isRwa"`
	}
	if err := json.Unmarshal(data, &contracts); err != nil {
		return nil, fmt.Errorf("parse contracts: %w", err)
	}

	catalog := &contractCatalog{
		bySymbol: make(map[string]contractEntry, len(contracts)),
		byBase:   make(map[string]string, len(contracts)),
	}
	rwaCount := 0
	for _, c := range contracts {
		sym := strings.ToUpper(strings.TrimSpace(c.Symbol))
		base := strings.ToUpper(strings.TrimSpace(c.BaseCoin))
		if sym == "" || base == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(c.QuoteCoin), "USDT") {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(c.SymbolStatus))
		if status != "" && status != "normal" && status != "listed" {
			continue
		}
		isRWA := strings.EqualFold(strings.TrimSpace(c.IsRWA), "YES")
		if isRWA {
			rwaCount++
		}
		entry := contractEntry{Symbol: sym, Base: base, IsRWA: isRWA}
		catalog.bySymbol[sym] = entry
		catalog.byBase[base] = sym
		// Also index HL-normalized aliases (e.g. display names -> ticker).
		if alias := hyperliquid.NormalizeXYZAlias(base); alias != "" && alias != base {
			if _, exists := catalog.byBase[alias]; !exists {
				catalog.byBase[alias] = sym
			}
		}
	}
	if len(catalog.bySymbol) == 0 {
		return nil, fmt.Errorf("empty Bitget contract catalog")
	}

	t.contractCatalog = catalog
	t.tradableCatalogTime = time.Now()
	// Legacy bool map for any older callers.
	t.tradableSymbols = make(map[string]bool, len(catalog.bySymbol))
	for sym := range catalog.bySymbol {
		t.tradableSymbols[sym] = true
	}

	logger.Infof("📋 Bitget: loaded %d USDT-FUTURES contracts (%d RWA), %d baseCoin mappings",
		len(catalog.bySymbol), rwaCount, len(catalog.byBase))
	return catalog, nil
}
