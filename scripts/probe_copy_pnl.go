//go:build ignore

package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	hlprovider "nofx/provider/hyperliquid"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	leader := "0x66f889094739dbb7d20aa60f645acd88feba75a9"
	st, err := hlprovider.FetchAccountState(ctx, leader)
	if err != nil {
		panic(err)
	}
	fmt.Printf("leader equity=$%.2f legs=%d\n", st.Equity, len(st.Legs))
	sort.Slice(st.Legs, func(i, j int) bool {
		return st.Legs[i].NotionalUSD > st.Legs[j].NotionalUSD
	})
	for _, leg := range st.Legs {
		fmt.Printf("  %s %s notional=$%.0f lev=%dx entry=$%.2f\n",
			leg.Symbol, leg.Side, leg.NotionalUSD, leg.Leverage, leg.EntryPrice)
	}
}
