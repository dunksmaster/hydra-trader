package store

import "strings"

// DefaultCryptoCoins is the fallback crypto universe for any AI trader that
// still has a Vergex/Claw402 coin source saved in its JSON. HYPE is omitted so
// Bitget and Hyperliquid can share the same list.
var DefaultCryptoCoins = []string{"BTC", "ETH", "SOL", "BNB", "XRP", "DOGE", "ADA"}

// ApplyStaticCryptoSource switches a strategy off the Claw402/Vergex board and
// onto a fixed crypto list, so no paid board lookup is needed.
func ApplyStaticCryptoSource(cfg *StrategyConfig) {
	if cfg == nil {
		return
	}
	cfg.CoinSource.SourceType = "static"
	if len(cfg.CoinSource.StaticCoins) == 0 {
		cfg.CoinSource.StaticCoins = append([]string{}, DefaultCryptoCoins...)
	}
	cfg.CoinSource.UseAI500 = false
	cfg.CoinSource.UseOITop = false
	cfg.CoinSource.UseOILow = false
	cfg.CoinSource.UseHyperAll = false
	cfg.CoinSource.UseHyperMain = false
	cfg.CoinSource.VergexLimit = 0
	cfg.CoinSource.VergexMarketType = ""
	cfg.CoinSource.VergexChain = ""
	cfg.CoinSource.VergexLiqBand = ""
	cfg.NormalizeProductSchema()
	cfg.ClampLimits()
}

// IsNVIDIAModel reports whether a saved model is the NVIDIA NIM / Nemotron
// decision engine (stored as an OpenAI-compatible custom endpoint).
func IsNVIDIAModel(model *AIModel) bool {
	if model == nil {
		return false
	}
	blob := strings.ToLower(strings.TrimSpace(model.CustomModelName + " " + model.CustomAPIURL + " " + model.Name))
	return strings.Contains(blob, "nvidia") || strings.Contains(blob, "nemotron")
}

func isClaw402Provider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "claw402")
}
