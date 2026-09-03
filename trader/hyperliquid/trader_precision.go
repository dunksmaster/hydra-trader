package hyperliquid

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const hyperliquidPriceSigFigs = 5

// FormatHyperliquidPerpPrice rounds a price to Hyperliquid perp rules:
// at most 5 significant figures and at most (6 - szDecimals) decimal places.
func FormatHyperliquidPerpPrice(price float64, szDecimals int) float64 {
	if price == 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return price
	}
	maxDecimals := maxPriceDecimals(szDecimals)
	rounded := roundToSigFigs(price, hyperliquidPriceSigFigs)
	return roundToDecimalPlaces(rounded, maxDecimals)
}

// WireSafeHyperliquidPerpPrice returns a float that serializes cleanly for HL wire format.
func WireSafeHyperliquidPerpPrice(price float64, szDecimals int) float64 {
	formatted := FormatHyperliquidPerpPrice(price, szDecimals)
	maxDecimals := maxPriceDecimals(szDecimals)
	s := fmt.Sprintf("%.*f", maxDecimals, formatted)
	parsed, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return formatted
	}
	return parsed
}

// IsValidHyperliquidPerpPrice reports whether price already satisfies HL perp tick rules.
func IsValidHyperliquidPerpPrice(price float64, szDecimals int) bool {
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return false
	}
	return price == WireSafeHyperliquidPerpPrice(price, szDecimals)
}

// PriceWireString formats a HL-valid price for direct OrderWire LimitPx/TriggerPx fields.
func PriceWireString(price float64, szDecimals int) string {
	safe := WireSafeHyperliquidPerpPrice(price, szDecimals)
	maxDecimals := maxPriceDecimals(szDecimals)
	result := fmt.Sprintf("%.*f", maxDecimals, safe)
	result = strings.TrimRight(result, "0")
	result = strings.TrimRight(result, ".")
	if result == "" || result == "-0" {
		return "0"
	}
	return result
}

func maxPriceDecimals(szDecimals int) int {
	maxDecimals := 6 - szDecimals
	if maxDecimals < 0 {
		return 0
	}
	return maxDecimals
}

func roundToSigFigs(price float64, sigfigs int) float64 {
	if price == 0 {
		return 0
	}
	magnitude := math.Abs(price)
	multiplier := 1.0
	for magnitude >= 10 {
		magnitude /= 10
		multiplier /= 10
	}
	for magnitude < 1 {
		magnitude *= 10
		multiplier *= 10
	}
	for i := 0; i < sigfigs-1; i++ {
		multiplier *= 10
	}
	return math.Round(price*multiplier) / multiplier
}

func roundToDecimalPlaces(price float64, decimals int) float64 {
	if decimals <= 0 {
		return math.Round(price)
	}
	multiplier := math.Pow10(decimals)
	return math.Round(price*multiplier) / multiplier
}
