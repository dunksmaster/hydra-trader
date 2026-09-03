//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	hlprovider "nofx/provider/hyperliquid"
)

type rawFill struct {
	Coin string `json:"coin"`
	Dir  string `json:"dir"`
	Side string `json:"side"`
	Px   string `json:"px"`
	Sz   string `json:"sz"`
	Time int64  `json:"time"`
	Tid  int64  `json:"tid"`
}

func main() {
	addr := os.Getenv("LEADER_ADDRESS")
	if addr == "" && len(os.Args) > 1 {
		addr = os.Args[1]
	}
	if addr == "" {
		addr = "0x66f889094739dbb7d20aa60f645acd88feba75a9"
	}
	addr = strings.ToLower(strings.TrimSpace(addr))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := hlprovider.FetchAccountStateAll(ctx, addr)
	if err != nil {
		panic(err)
	}
	fmt.Printf("=== Leader %s ===\n", shortAddr(addr))
	fmt.Printf("equity=$%.2f open_legs=%d\n", st.Equity, len(st.Legs))
	sort.Slice(st.Legs, func(i, j int) bool {
		return st.Legs[i].NotionalUSD > st.Legs[j].NotionalUSD
	})
	for _, leg := range st.Legs {
		fmt.Printf("  %s %s notional=$%.0f lev=%dx entry=$%.2f\n",
			leg.Symbol, leg.Side, leg.NotionalUSD, leg.Leverage, leg.EntryPrice)
	}

	var fills []rawFill
	if err := hlprovider.PostInfo(ctx, map[string]any{
		"type": "userFills",
		"user": addr,
	}, &fills); err != nil {
		panic(err)
	}

	now := time.Now().UTC()
	cut7 := now.Add(-7 * 24 * time.Hour).UnixMilli()
	cut30 := now.Add(-30 * 24 * time.Hour).UnixMilli()
	cut90 := now.Add(-90 * 24 * time.Hour).UnixMilli()

	var n7, n30, n90 int
	coins7 := map[string]int{}
	for _, f := range fills {
		if f.Time >= cut90 {
			n90++
		}
		if f.Time >= cut30 {
			n30++
		}
		if f.Time >= cut7 {
			n7++
			coins7[f.Coin]++
		}
	}

	fmt.Printf("\n=== Fill activity (userFills sample=%d) ===\n", len(fills))
	fmt.Printf("fills_7d=%d (~%.1f/day)\n", n7, float64(n7)/7.0)
	fmt.Printf("fills_30d=%d (~%.1f/day)\n", n30, float64(n30)/30.0)
	fmt.Printf("fills_90d=%d (~%.1f/day)\n", n90, float64(n90)/90.0)

	if len(coins7) > 0 {
		type kv struct {
			k string
			v int
		}
		var top []kv
		for k, v := range coins7 {
			top = append(top, kv{k, v})
		}
		sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })
		fmt.Printf("coins_7d:")
		for i, t := range top {
			if i >= 6 {
				break
			}
			fmt.Printf(" %s(%d)", t.k, t.v)
		}
		fmt.Println()
	}

	if len(fills) > 0 {
		sort.Slice(fills, func(i, j int) bool { return fills[i].Time > fills[j].Time })
		fmt.Printf("\nlast_fill=%s %s %s @ %s\n",
			time.UnixMilli(fills[0].Time).UTC().Format(time.RFC3339),
			fills[0].Coin, fills[0].Dir, fills[0].Px)
	}
}

func shortAddr(a string) string {
	if len(a) <= 12 {
		return a
	}
	return a[:6] + "..." + a[len(a)-4:]
}
