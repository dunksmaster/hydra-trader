# Hydra Trader — Architecture Beyond Upstream NOFX

**Fork of:** [NoFxAiOS/nofx](https://github.com/NoFxAiOS/nofx) (AGPL-3.0)

Upstream NOFX already documents its own architecture in `docs/architecture/` — this file
covers only what's original to this fork: an AI-decision trading core extended with a
multi-leader Hyperliquid copy-trading engine.

## Why "Hydra"

One account, many heads: a single AI-decision trader (Bitget) runs alongside a fleet of
independent copy-trading "heads," each mirroring a different Hyperliquid leader wallet,
all sharing one margin pool with a wallet-wide safety cap that no individual head can
exceed.

## Copy-trading engine — leader fill to follower order

```mermaid
flowchart TD
    L1[Leader Wallet 1] -->|WebSocket fills| W1[CopyLeaderWatcher]
    L2[Leader Wallet 2] -->|WebSocket fills| W2[CopyLeaderWatcher]
    LN[Leader Wallet N] -->|WebSocket fills| WN[CopyLeaderWatcher]

    W1 --> DD1{tid seen<br/>before?}
    W2 --> DD2{tid seen<br/>before?}
    WN --> DDN{tid seen<br/>before?}

    DD1 -->|yes: drop| X1[discarded]
    DD2 -->|yes: drop| X2[discarded]
    DDN -->|yes: drop| XN[discarded]

    DD1 -->|no| HF[handleLeaderFill]
    DD2 -->|no| HF
    DDN -->|no| HF

    HF --> SB{"Copy paused /<br/>layer &gt;= 3?"}
    SB -->|yes| SkipA[skip: paused]

    SB -->|no| PH{Follower already<br/>holds this side?}
    PH -->|yes| Overflow[enqueueOverflowOpen<br/>skip: already_open]

    PH -->|no| SZ[ComputeCopyNotionalUSD<br/>size relative to follower equity]
    SZ -->|sizing rejected| Overflow

    SZ -->|ok| WS{"Wallet slots full?<br/>(wallet_copy_slots)"}
    WS -->|yes: shared cap hit| SkipB["skip: wallet slots full<br/>(blocks ALL heads equally)"]

    WS -->|no| PC{"Per-leader max_positions<br/>reached?"}
    PC -->|yes| SkipC[skip: max positions]

    PC -->|no| EXEC[Place real order<br/>on Hyperliquid]
    EXEC --> TG[Telegram alert<br/>on skip/paused/dropped]

    style WS fill:#E0483B,color:#fff
    style DD1 fill:#1A1813,color:#fff
    style DD2 fill:#1A1813,color:#fff
    style DDN fill:#1A1813,color:#fff
```

**The one thing worth understanding about this design:** the wallet-wide slot cap
(`wallet_copy_slots`) is checked *before* any individual head's own `max_positions`
cap. That means N leaders sharing one Hyperliquid wallet can never collectively exceed
the wallet's real capacity, even if every individual head's own cap would allow more —
the shared resource is protected first, per-head limits are secondary.

## System alerting

```mermaid
flowchart LR
    subgraph "Failure conditions detected in the trading loop"
        A1[AI wallet empty]
        A2[AI quota exhausted]
        A3[AI rate-limited]
        A4[Safe Mode activated]
        A5[Safe Mode deactivated]
    end
    A1 & A2 & A3 & A4 & A5 --> EV[events.SystemAlertEvent]
    EV --> TN[Telegram notifier]
    TN --> USER[User's Telegram chat]
```

Upstream NOFX had no equivalent — failures were previously visible only in server logs.

## Known limitations (see PUBLISH-CHECKLIST.md for the full list)

- All order execution uses IOC (Immediate-Or-Cancel) — no maker-fee path exists anywhere
  in the copy-trading or AI-decision order flow.
- No backtesting — every strategy change is validated live, with real money.
