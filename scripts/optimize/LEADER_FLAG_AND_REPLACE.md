# Leader flag, pause, and replace

Copy trading stays on. This rule only pauses a *bad streak* leader and leaves a free slot. Other bots keep running. Do not apply live until you accept it.

## Rule

Track **completed copy trades** per leader (follower realized net after fees, not the leader's giant inventory PnL).

| Streak | Action |
|---|---|
| 5 losses in a row | **FLAG**. Telegram names the leader, win rate, last 10 results. Copy still runs. |
| 10 losses in a row | **PAUSE**. Set that bot to `dry_run` / `copy_paused=true`. Do not open new copies. Still close existing legs if the leader closes. Other leaders keep copying. |
| Next win | Reset the loss streak to 0. A flagged leader can recover. A paused leader stays paused until you unpause or replace. |

Also show a rolling **win rate** (last 20 completed copies, and all-time) on `/leaders` or `/copystatus`.

When paused, that slot is **empty**:

- Telegram: `Slot free — send a new leader address to replace 0x...`
- You paste a Hyperdash wallet
- We PUT only that strategy's `leader_address` and clear pause/streak
- Do not restart all five bots

Default thresholds: flag=5, pause=10. Configurable on `CopyStrategyConfig`.

## Why consecutive losses, not raw win rate

These Hyperdash leaders print huge size. Their public win rate can stay high while *your* $50 copies lose on noise and fees. The streak is measured on **your** closed copies.

Ignore:

- skipped opens (`already_open`, `margin`, `max_positions`)
- overflow-only legs (count on BigG separately, or not at all for the HL leader score)
- manual closes you took

## Code (when you say implement)

1. `store.CopyStrategyConfig`: `flag_loss_streak`, `pause_loss_streak`, `copy_paused`, `loss_streak`, `wins`, `losses`.
2. On each successful copy close in `trader/auto_trader_copy_fills.go`, update streak + win/loss.
3. New alerts in `events/alert.go`: `leader_flagged`, `leader_paused`.
4. Telegram `/leaders`: name, short address, WR, streak, FLAG/PAUSED/OK, empty slot line.
5. `/useleader 0x...` or callback to fill the paused slot (bound chat only).
6. Tests: 5th loss flags, 10th pauses, a win resets streak, other bots unaffected.

Live Railway config is unchanged until you approve.

## Hyperdash source

List: https://hyperdash.com/copytrading (same data as https://hyperdash.com/explore/copytraders).
API used: `https://api.hyperdash.com/graphql` `exploreTraders` sorted by `copyScore`, timeframe `seven_days`.
Last-24h activity: public Hyperliquid `userFillsByTime`.
