# Strategy B MAX (Plan B)

Hedge-fund copy rules for a small wallet. Backtested on public Hyperliquid leader fills (90 days) plus 1h candles. No live settings were changed.

## What the leaders actually do

They are not clean “one trade for a month” accounts. Public fills:

| Leader | Fills | Style |
|---|---|---|
| Leviathan | 36,380 | High-frequency inventory on xyz stocks (SKHX, MU, SNDK) plus some BTC |
| Alpha 6859 | 15,410 | High-frequency shorts on ZEC, memes, HIP-3 `@` coins |
| Grinder | 5,855 | Almost all xyz:CL inventory |
| Copy L4 | 3,138 | Mix of xyz stocks + ETH/BTC |
| Money Printer | 0 | This address has no public fills |

Their own books print huge closed PnL because they trade huge size. A $50 copy does **not** inherit that PnL.

## Why you are losing now

Simulated **current live setup** ($50, 10x, 1.5% safety stop, copy every flatten/add the wallet can hold):

| Wallet | Return | Max DD | Win rate | Liq-risk alerts |
|---|---|---|---|---|
| $15 (your HL size) | **-46.4%** | **65%** | 33% | **8** |
| $100 | +5.7% | 15% | 34% | 0 |

That matches what you feel: automatic closes lose, margin hits 97%, the two manual winners were the ones you held through noise.

The leak is structural:

1. Leaders add to inventory thousands of times. The bot treats those as new $50 / 10x trades.
2. A 1.5% adverse tick (15% margin at 10x) stops you out. Their winners often dip 3–19% first.
3. Five bots share one ~$15 wallet, so one coin can be copied into a liquidation-risk book.

## The copyable edge (this is the real number)

If you only copy **flat → position → flat** cycles (ignore HFT add-ons) at $50, no extra stop:

| Universe | Trades | Net | Win rate |
|---|---|---|---|
| All flatten cycles | 115 | **+$64.73** | 53.9% |
| Crypto only (no xyz / @) | 10 | **+$25.14** | 90.0% |

So the leaders *do* have a copyable edge. The current bot is not copying that edge. It is copying inventory noise.

## Strategy B MAX rules

Name: **Strategy B MAX** (Plan B)

1. **Copy flatten cycles only.** Open when the leader goes from flat to a position. Close when they flatten or flip. Never copy add-ons (`already_open`).
2. **Two books, one wallet.**
   - Core (swings ≥ 24h): 3x, 2% equity risk, stop beyond winner MAE (~15–20% price on xyz swings, never 1.5%).
   - Tactical (scalps): off until equity ≥ $80. Then 8x, 0.75% risk, mirror leader exit, no extra profit cap.
3. **Bull regime (BTC now):** full-size longs, skip new core shorts.
4. **Scale-out on core winners:** bank 25% at +100% margin PnL, 25% at +300%, ratchet the rest to breakeven. Do not hard-take-profit a runner.
5. **Margin reserve 30%.** Hard reject new copies before another 97% alert.
6. **Max 3 core + 2 tactical.** One position per coin+side across all five bots.
7. **xyz / HIP-3:** allowed only as core flatten cycles after equity ≥ $100. They cannot overflow to Bitget.
8. **Money Printer:** pause until a working leader address is found.
9. **AI (NVIDIA / claw402):** advisory only. Classify scalp vs swing, suggest hold/trim/close. Cannot widen a stop or add size. Turn on after dry-run.

## Simulated B MAX vs current

| Setup | Return | Max DD | Win rate | Liq alerts |
|---|---|---|---|---|
| Current @ $15 | -46.4% | 65% | 33% | 8 |
| Current @ $100 | +5.7% | 15% | 34% | 0 |
| B MAX flatten @ $100 | **+7.5%** | **2.1%** | **58%** | **0** |
| Perfect $50 flatten mirror | +$64.73 | — | 54% | — |

B MAX is the risk-controlled version of the flatten-mirror edge. The $15 wallet is below the minimum for two-book trading ($12 min size vs 2% risk). **Fund Hyperliquid to at least $80–$100, or the math stays blocked.**

## Do not do this

- Do not add a +2% profit lock. That keeps every leader loss and cuts the runners.
- Do not copy every leader fill.
- Do not run 5 bots at $50 / 10x on a $15 wallet.
- Do not turn NVIDIA AI into the risk manager.

## Next live steps (not applied)

1. Keep BigG on. Keep Autopilot off.
2. Raise HL funds or cut live copy size to $12 at 3x until equity ≥ $100.
3. Paper / dry-run Strategy B MAX flatten-only on Leviathan + Copy L4 first.
4. Then enable the other leaders one at a time.

Artifacts: `scripts/optimize/data/strategy_b_max_report.json` and cached fills/candles.
Run again: `python scripts/optimize/strategy_b_max.py`
