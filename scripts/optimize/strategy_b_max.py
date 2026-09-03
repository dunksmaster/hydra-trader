"""Strategy B MAX (Plan B) — hedge-fund copy simulator.

Public data only (Hyperliquid fills + candles, Binance fallback).
No live config changes. No paid APIs required.
"""

from __future__ import annotations

import csv
import json
import math
import os
import time
import urllib.error
import urllib.request
from collections import defaultdict
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from typing import Any

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data")
STRATEGY_NAME = "Strategy B MAX"
PLAN_NAME = "Plan B"

HL_INFO = "https://api.hyperliquid.xyz/info"
BINANCE_KLINES = "https://fapi.binance.com/fapi/v1/klines"
FEE_RATE = 0.000784
MIN_NOTIONAL = 12.0

LEADERS = [
    {"name": "Leviathan", "address": "0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf"},
    {"name": "Grinder", "address": "0xdebbea84972174f44778a00521b1b5faa663abbb"},
    {"name": "Money Printer", "address": "0x8a0cd16a004e21e04936a0a01c6f9a49ff937914"},
    {"name": "Copy L4", "address": "0x6a02aedceac5a6813d960e4dae1910d9c458e77c"},
    {"name": "Alpha 6859", "address": "0x6859da14835424957a1e6b397d8026b1d9ff7e1e"},
]

# Current live copy setup
CURRENT = {
    "notional": 50.0,
    "leverage": 10.0,
    "safety_stop_price_pct": 1.5,  # 15% margin / 10x
    "max_positions": 10,
    "start_equity": 15.0,
}

# Strategy B MAX starting hypotheses (levels refined after MAE/MFE)
BMAX = {
    "start_equity": 100.0,
    "reserve_pct": 0.30,
    "heat_pct": 0.08,
    "core_risk_pct": 0.02,
    "tactical_risk_pct": 0.0075,
    "core_leverage": 3.0,
    "tactical_leverage": 8.0,
    "max_core": 3,
    "max_tactical": 2,
    "ladder_1": 1.00,   # +100% margin PnL
    "ladder_2": 3.00,   # +300% margin PnL
    "ladder_frac": 0.25,
    "scalp_hours": 8.0,
    "swing_hours": 24.0,
    "mae_stop_quantile": 0.90,
    "min_mae_stop_pct": 2.5,
    "drawdown_half": 0.10,
    "drawdown_off_tactical": 0.20,
}


def utc_ms(dt: datetime | None = None) -> int:
    dt = dt or datetime.now(timezone.utc)
    return int(dt.timestamp() * 1000)


def iso(ms: int) -> str:
    return datetime.fromtimestamp(ms / 1000, tz=timezone.utc).isoformat()


def ensure_data() -> None:
    os.makedirs(DATA, exist_ok=True)


def http_json(method: str, url: str, payload: Any | None = None, retries: int = 4) -> Any:
    body = None
    headers = {"User-Agent": "nofx-strategy-b-max/1.0", "Accept": "application/json"}
    if payload is not None:
        body = json.dumps(payload).encode()
        headers["Content-Type"] = "application/json"
    last_err = None
    for attempt in range(retries):
        req = urllib.request.Request(url, data=body, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=45) as resp:
                return json.loads(resp.read().decode())
        except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError, json.JSONDecodeError) as err:
            last_err = err
            time.sleep(1.2 * (attempt + 1))
    raise RuntimeError(f"{method} {url} failed: {last_err}")


def hl_info(payload: dict) -> Any:
    return http_json("POST", HL_INFO, payload)


def parse_fill_action(fill: dict) -> str:
    direction = str(fill.get("dir") or "").strip().lower()
    mapping = {
        "open long": "open_long",
        "open short": "open_short",
        "close long": "close_long",
        "close short": "close_short",
        "long > short": "close_long",
        "short > long": "close_short",
    }
    if direction in mapping:
        return mapping[direction]
    side = str(fill.get("side") or "").upper()
    closed = float(fill.get("closedPnl") or 0)
    is_buy = side in ("B", "BUY", "BID")
    if closed != 0:
        return "close_short" if is_buy else "close_long"
    return "open_long" if is_buy else "open_short"


def fetch_user_fills(address: str, days: int = 90) -> list[dict]:
    start = utc_ms() - days * 24 * 60 * 60 * 1000
    end = utc_ms()
    out: list[dict] = []
    cursor = start
    while cursor < end:
        chunk_end = min(end, cursor + 30 * 24 * 60 * 60 * 1000)
        try:
            raw = hl_info({
                "type": "userFillsByTime",
                "user": address,
                "startTime": cursor,
                "endTime": chunk_end,
            })
        except RuntimeError:
            raw = hl_info({"type": "userFills", "user": address})
            if isinstance(raw, list):
                raw = [f for f in raw if int(f.get("time") or 0) >= start]
            return raw if isinstance(raw, list) else []
        if not isinstance(raw, list) or not raw:
            cursor = chunk_end + 1
            continue
        out.extend(raw)
        last_t = max(int(f.get("time") or 0) for f in raw)
        if last_t <= cursor:
            cursor = chunk_end + 1
        else:
            cursor = last_t + 1
        if len(raw) < 50:
            cursor = chunk_end + 1
        time.sleep(0.15)
    # unique by tid/hash/time
    seen = set()
    uniq = []
    for f in out:
        key = (f.get("tid"), f.get("hash"), f.get("time"), f.get("coin"), f.get("sz"))
        if key in seen:
            continue
        seen.add(key)
        uniq.append(f)
    uniq.sort(key=lambda x: int(x.get("time") or 0))
    return uniq


def coin_to_binance(coin: str) -> str | None:
    c = coin.replace("xyz:", "").upper()
    if c.endswith("USDT"):
        return c
    skip = {"PURR", "HYPE"}  # try anyway
    return f"{c}USDT"


def fetch_hl_candles(coin: str, interval: str, start_ms: int, end_ms: int) -> list[tuple]:
    rows = []
    cursor = start_ms
    while cursor < end_ms:
        raw = hl_info({
            "type": "candleSnapshot",
            "req": {
                "coin": coin,
                "interval": interval,
                "startTime": cursor,
                "endTime": end_ms,
            },
        })
        if not isinstance(raw, list) or not raw:
            break
        for c in raw:
            t = int(c.get("t") or 0)
            rows.append((
                t,
                float(c["o"]),
                float(c["h"]),
                float(c["l"]),
                float(c["c"]),
                float(c.get("v") or 0),
            ))
        last_t = int(raw[-1].get("T") or raw[-1].get("t") or cursor)
        nxt = last_t + 1
        if nxt <= cursor or len(raw) < 200:
            break
        cursor = nxt
        time.sleep(0.12)
    # unique
    by_t = {r[0]: r for r in rows}
    return [by_t[k] for k in sorted(by_t)]


def fetch_binance_candles(symbol: str, interval: str, start_ms: int, end_ms: int) -> list[tuple]:
    rows = []
    cursor = start_ms
    while cursor < end_ms:
        url = (
            f"{BINANCE_KLINES}?symbol={symbol}&interval={interval}"
            f"&startTime={cursor}&endTime={end_ms}&limit=1500"
        )
        try:
            raw = http_json("GET", url)
        except RuntimeError:
            break
        if not isinstance(raw, list) or not raw:
            break
        for item in raw:
            rows.append((
                int(item[0]),
                float(item[1]),
                float(item[2]),
                float(item[3]),
                float(item[4]),
                float(item[5]),
            ))
        last_close = int(raw[-1][6])
        nxt = last_close + 1
        if nxt <= cursor or len(raw) < 1500:
            break
        cursor = nxt
        time.sleep(0.12)
    by_t = {r[0]: r for r in rows}
    return [by_t[k] for k in sorted(by_t)]


@dataclass
class LeaderTrade:
    leader: str
    address: str
    coin: str
    side: str
    entry_ms: int
    exit_ms: int
    entry: float
    exit: float
    qty: float
    notional: float
    closed_pnl: float
    fees: float
    style: str = "unknown"
    mae_pct: float = 0.0
    mfe_pct: float = 0.0
    hold_hours: float = 0.0


def signed_size(fill: dict) -> float:
    sz = float(fill.get("sz") or 0)
    side = str(fill.get("side") or "").upper()
    if side in ("B", "BUY", "BID"):
        return sz
    return -sz


def reconstruct_trades(leader: str, address: str, fills: list[dict]) -> list[LeaderTrade]:
    """Inventory-aware reconstruction.

    High-frequency leaders almost never go flat. We emit:
    1. flatten/flip cycles when startPosition crosses zero
    2. realized close slices (closedPnl != 0) as scalp/intraday units
    """
    trades: list[LeaderTrade] = []
    cycle: dict[str, dict] = {}

    def style_of(hours: float) -> str:
        if hours < BMAX["scalp_hours"]:
            return "scalp"
        if hours >= BMAX["swing_hours"]:
            return "swing"
        return "intraday"

    def emit(coin: str, side: str, entry_ms: int, exit_ms: int, entry: float, exit_px: float,
             qty: float, pnl: float, fee: float) -> None:
        if entry <= 0 or qty <= 0 or exit_ms <= 0:
            return
        hold = max(0.0, (exit_ms - entry_ms) / 3600000.0)
        trades.append(LeaderTrade(
            leader=leader, address=address, coin=coin, side=side,
            entry_ms=entry_ms, exit_ms=exit_ms, entry=entry, exit=exit_px,
            qty=qty, notional=entry * qty, closed_pnl=pnl, fees=fee,
            style=style_of(hold), hold_hours=hold,
        ))

    for f in sorted(fills, key=lambda x: int(x.get("time") or 0)):
        coin = str(f.get("coin") or "")
        px = float(f.get("px") or 0)
        sz = float(f.get("sz") or 0)
        t = int(f.get("time") or 0)
        if not coin or px <= 0 or sz <= 0:
            continue
        start = float(f.get("startPosition") or 0)
        after = start + signed_size(f)
        fee = float(f.get("fee") or 0)
        pnl = float(f.get("closedPnl") or 0)
        side = "long" if after > 0 or (abs(after) < 1e-9 and start > 0) else "short"
        if abs(start) < 1e-8 and abs(after) > 1e-8:
            cycle[coin] = {"side": "long" if after > 0 else "short", "entry": px, "entry_ms": t, "qty": abs(after), "pnl": 0.0, "fee": fee}
        elif coin in cycle:
            c = cycle[coin]
            c["pnl"] += pnl
            c["fee"] += fee
            crossed = (start > 0 and after <= 0) or (start < 0 and after >= 0)
            if crossed:
                emit(coin, c["side"], c["entry_ms"], t, c["entry"], px, c["qty"], c["pnl"], c["fee"])
                if abs(after) > 1e-8:
                    cycle[coin] = {"side": "long" if after > 0 else "short", "entry": px, "entry_ms": t, "qty": abs(after), "pnl": 0.0, "fee": 0.0}
                else:
                    cycle.pop(coin, None)

        # realized slice — the actual P&L unit for HFT inventory
        if pnl != 0:
            close_side = "long" if "long" in parse_fill_action(f) else "short"
            # approximate entry from start position and close
            entry = px
            if close_side == "long":
                entry = px - (pnl / sz) if sz else px
            else:
                entry = px + (pnl / sz) if sz else px
            if entry <= 0:
                entry = px
            # hold unknown for a single slice; treat as scalp unless gap from cycle
            hold_from = cycle.get(coin, {}).get("entry_ms", t)
            emit(coin, close_side, hold_from, t, entry, px, sz, pnl, fee)

    return trades


def copyable_events(fills_by_leader: dict[str, list[dict]]) -> list[dict]:
    """Events a $50 copy bot can actually follow: new inventory from flat, then first flatten.

    Skip HFT add-ons (already_open). Skip xyz: for the crypto book; keep them tagged.
    """
    events = []
    for leader, fills in fills_by_leader.items():
        by_coin: dict[str, list] = defaultdict(list)
        for f in fills:
            by_coin[str(f.get("coin") or "")].append(f)
        for coin, rows in by_coin.items():
            rows = sorted(rows, key=lambda x: int(x.get("time") or 0))
            open_side = None
            open_px = 0.0
            open_t = 0
            for f in rows:
                start = float(f.get("startPosition") or 0)
                after = start + signed_size(f)
                px = float(f.get("px") or 0)
                t = int(f.get("time") or 0)
                if px <= 0:
                    continue
                if open_side is None and abs(start) < 1e-6 and abs(after) > 1e-6:
                    open_side = "long" if after > 0 else "short"
                    open_px, open_t = px, t
                    events.append({"t": t, "coin": coin, "action": f"open_{open_side}", "px": px, "leader": leader, "style": "unknown", "hft": coin.startswith("xyz:") or coin.startswith("@")})
                elif open_side is not None:
                    flat = abs(after) < 1e-6
                    flip = (open_side == "long" and after < 0) or (open_side == "short" and after > 0)
                    if flat or flip:
                        events.append({"t": t, "coin": coin, "action": f"close_{open_side}", "px": px, "leader": leader, "style": "unknown", "hft": coin.startswith("xyz:") or coin.startswith("@")})
                        open_side = None
                        if flip and abs(after) > 1e-6:
                            open_side = "long" if after > 0 else "short"
                            events.append({"t": t, "coin": coin, "action": f"open_{open_side}", "px": px, "leader": leader, "style": "unknown", "hft": coin.startswith("xyz:") or coin.startswith("@")})
    events.sort(key=lambda x: (x["t"], 0 if x["action"].startswith("open") else 1))
    pending: dict[tuple[str, str, str], dict] = {}
    for e in events:
        k = (e["leader"], e["coin"], e["action"].split("_")[-1])
        if e["action"].startswith("open_"):
            pending[k] = e
        elif e["action"].startswith("close_") and k in pending:
            hold_h = max(0.0, (e["t"] - pending[k]["t"]) / 3600000.0)
            style = "scalp" if hold_h < BMAX["scalp_hours"] else ("swing" if hold_h >= BMAX["swing_hours"] else "intraday")
            pending[k]["style"] = style
            e["style"] = style
            pending.pop(k, None)
    return events


class CandleStore:
    def __init__(self) -> None:
        self.rows: dict[str, list[tuple]] = {}

    def load_or_fetch(self, coins: list[str], start_ms: int, end_ms: int) -> None:
        path = os.path.join(DATA, "candles_1h.json")
        cached = {}
        if os.path.exists(path):
            with open(path, encoding="utf-8") as f:
                cached = json.load(f)
        for coin in coins:
            key = f"{coin}|1h"
            if key in cached and len(cached[key]) > 50:
                self.rows[coin] = [tuple(x) for x in cached[key]]
                continue
            rows = fetch_hl_candles(coin, "1h", start_ms, end_ms)
            if len(rows) < 20:
                bsym = coin_to_binance(coin)
                if bsym:
                    brows = fetch_binance_candles(bsym, "1h", start_ms, end_ms)
                    if len(brows) > len(rows):
                        rows = brows
            self.rows[coin] = rows
            cached[key] = rows
            print(f"  candles {coin}: {len(rows)}")
        with open(path, "w", encoding="utf-8") as f:
            json.dump(cached, f)

    def window(self, coin: str, start_ms: int, end_ms: int) -> list[tuple]:
        rows = self.rows.get(coin) or []
        return [r for r in rows if start_ms <= r[0] <= end_ms]


def excursion(trade: LeaderTrade, store: CandleStore) -> None:
    rows = store.window(trade.coin, trade.entry_ms, trade.exit_ms or trade.entry_ms)
    if not rows:
        return
    mae = 0.0
    mfe = 0.0
    for _, _, high, low, _, _ in rows:
        if trade.side == "long":
            adverse = (trade.entry - low) / trade.entry
            favor = (high - trade.entry) / trade.entry
        else:
            adverse = (high - trade.entry) / trade.entry
            favor = (trade.entry - low) / trade.entry
        mae = max(mae, adverse)
        mfe = max(mfe, favor)
    trade.mae_pct = mae * 100
    trade.mfe_pct = mfe * 100


def quantile(xs: list[float], q: float) -> float:
    if not xs:
        return 0.0
    ys = sorted(xs)
    i = min(len(ys) - 1, max(0, int(round((len(ys) - 1) * q))))
    return ys[i]


def ema(values: list[float], n: int) -> list[float]:
    if not values:
        return []
    k = 2 / (n + 1)
    out = [values[0]]
    for v in values[1:]:
        out.append(v * k + out[-1] * (1 - k))
    return out


def regime_series(store: CandleStore) -> list[tuple[int, str]]:
    btc = store.rows.get("BTC") or []
    if len(btc) < 210:
        return []
    closes = [r[4] for r in btc]
    e50 = ema(closes, 50)
    e200 = ema(closes, 200)
    out = []
    for i, row in enumerate(btc):
        if i < 200:
            continue
        bull = closes[i] > e50[i] > e200[i] and e50[i] >= e50[i - 5]
        bear = closes[i] < e50[i] < e200[i] and e50[i] <= e50[i - 5]
        out.append((row[0], "bull" if bull else ("bear" if bear else "chop")))
    return out


def regime_at(series: list[tuple[int, str]], ts: int) -> str:
    if not series:
        return "unknown"
    last = series[0][1]
    for t, r in series:
        if t > ts:
            break
        last = r
    return last


def analyze_leaders(trades: list[LeaderTrade]) -> dict:
    report: dict[str, Any] = {}
    for leader in {t.leader for t in trades}:
        subset = [t for t in trades if t.leader == leader]
        by_style: dict[str, Any] = {}
        for style in ("scalp", "intraday", "swing"):
            rows = [t for t in subset if t.style == style]
            if not rows:
                continue
            wins = [t for t in rows if t.closed_pnl > 0]
            losses = [t for t in rows if t.closed_pnl <= 0]
            win_mae = [t.mae_pct for t in wins if t.mae_pct > 0]
            giveback = []
            for t in wins:
                if t.mfe_pct > 0.01:
                    # how much of MFE they kept (approx from closed vs MFE)
                    px_pnl = ((t.exit - t.entry) / t.entry) * (1 if t.side == "long" else -1) * 100
                    giveback.append(max(0.0, t.mfe_pct - px_pnl))
            net = sum(t.closed_pnl - t.fees for t in rows)
            verdict = "drop"
            if net > 0 and len(rows) >= 5:
                avg_give = sum(giveback) / len(giveback) if giveback else 0
                if style == "swing":
                    verdict = "copy_with_ladder" if avg_give > 2 else "copy_exactly"
                else:
                    verdict = "copy_exactly" if net > 0 else "drop"
            elif net > 0:
                verdict = "watch"
            by_style[style] = {
                "n": len(rows),
                "wins": len(wins),
                "win_rate": 100 * len(wins) / len(rows),
                "net": net,
                "avg_win": sum(t.closed_pnl for t in wins) / len(wins) if wins else 0,
                "avg_loss": sum(t.closed_pnl for t in losses) / len(losses) if losses else 0,
                "mae_p50": quantile([t.mae_pct for t in rows], 0.50),
                "mae_p90_winners": quantile(win_mae, 0.90),
                "mfe_p50": quantile([t.mfe_pct for t in rows], 0.50),
                "avg_giveback_pct": sum(giveback) / len(giveback) if giveback else 0,
                "verdict": verdict,
            }
        net_all = sum(t.closed_pnl - t.fees for t in subset)
        report[leader] = {
            "n": len(subset),
            "net": net_all,
            "verdict": "keep" if net_all > 0 else "drop",
            "styles": by_style,
        }
    return report


@dataclass
class SimTrade:
    variant: str
    book: str
    leader: str
    coin: str
    side: str
    entry: float
    exit: float
    notional: float
    pnl: float
    fees: float
    reason: str
    hold_hours: float


def price_move(side: str, entry: float, price: float) -> float:
    move = (price - entry) / entry
    return -move if side == "short" else move


class Position:
    def __init__(self, book, leader, coin, side, entry, qty, notional, lev, stop_pct, ts):
        self.book = book
        self.leader = leader
        self.coin = coin
        self.side = side
        self.entry = entry
        self.qty = qty
        self.notional = notional
        self.lev = lev
        self.stop_pct = stop_pct
        self.entry_ms = ts
        self.peak_mfe = 0.0
        self.scaled1 = False
        self.scaled2 = False
        self.realized = 0.0
        self.fees = 0.0

    def key(self) -> tuple[str, str]:
        return self.coin, self.side


def simulate_current(events: list[dict], store: CandleStore, start_equity: float) -> dict:
    equity = start_equity
    positions: dict[tuple[str, str], Position] = {}
    trades: list[SimTrade] = []
    peak = equity
    max_dd = 0.0
    liq_alerts = 0

    def close_pos(pos: Position, price: float, ts: int, reason: str, frac: float = 1.0) -> None:
        nonlocal equity
        qty = pos.qty * frac
        notion = pos.entry * qty
        move = price_move(pos.side, pos.entry, price)
        fees = notion * FEE_RATE * 2 if frac == 1.0 else notion * FEE_RATE
        pnl = notion * move - fees
        equity += pnl
        pos.realized += pnl
        pos.fees += fees
        pos.qty -= qty
        trades.append(SimTrade(
            "current", "shared", pos.leader, pos.coin, pos.side,
            pos.entry, price, notion, pnl, fees, reason,
            max(0.0, (ts - pos.entry_ms) / 3600000.0),
        ))
        if pos.qty <= 1e-9:
            positions.pop(pos.key(), None)

    for ev in events:
        ts, coin, action, px, leader = ev["t"], ev["coin"], ev["action"], ev["px"], ev["leader"]
        side = "long" if "long" in action else "short"
        key = (coin, side)
        # intra-event safety stops using last candle low/high approximation at event time
        for pos in list(positions.values()):
            rows = store.window(pos.coin, pos.entry_ms, ts)
            if not rows:
                continue
            high = max(r[2] for r in rows)
            low = min(r[3] for r in rows)
            if pos.side == "long":
                worst = (pos.entry - low) / pos.entry * 100
                if worst >= CURRENT["safety_stop_price_pct"]:
                    stop_px = pos.entry * (1 - CURRENT["safety_stop_price_pct"] / 100)
                    close_pos(pos, stop_px, ts, "safety_stop")
            else:
                worst = (high - pos.entry) / pos.entry * 100
                if worst >= CURRENT["safety_stop_price_pct"]:
                    stop_px = pos.entry * (1 + CURRENT["safety_stop_price_pct"] / 100)
                    close_pos(pos, stop_px, ts, "safety_stop")
        used = sum(p.notional / CURRENT["leverage"] for p in positions.values())
        if start_equity > 0 and used / max(equity, 1e-9) >= 0.80:
            liq_alerts += 1
        if action.startswith("open_"):
            if key in positions or len(positions) >= CURRENT["max_positions"]:
                continue
            notion = CURRENT["notional"]
            if notion < MIN_NOTIONAL:
                continue
            margin = notion / CURRENT["leverage"]
            if margin > max(0.0, equity - used):
                continue
            qty = notion / px
            positions[key] = Position("shared", leader, coin, side, px, qty, notion, CURRENT["leverage"], CURRENT["safety_stop_price_pct"], ts)
        else:
            pos = positions.get(key)
            if pos:
                close_pos(pos, px, ts, "leader_close")
        peak = max(peak, equity)
        if peak > 0:
            max_dd = max(max_dd, (peak - equity) / peak)
    return summarize("current", trades, equity, start_equity, max_dd, liq_alerts)


def simulate_bmax(events: list[dict], store: CandleStore, analysis: dict, regimes: list[tuple[int, str]], start_equity: float) -> dict:
    cfg = BMAX
    equity = start_equity
    peak = equity
    max_dd = 0.0
    liq_alerts = 0
    tactical_on = True
    positions: dict[tuple[str, str], Position] = {}
    trades: list[SimTrade] = []
    keep_leaders = {k for k, v in analysis.items() if v.get("verdict") == "keep"}
    if not keep_leaders:
        keep_leaders = set(analysis)

    def stop_for(leader: str, style: str) -> float:
        styles = analysis.get(leader, {}).get("styles", {})
        mae = styles.get(style, {}).get("mae_p90_winners") or styles.get("swing", {}).get("mae_p90_winners") or 0
        return max(cfg["min_mae_stop_pct"], mae * 1.15 if mae else cfg["min_mae_stop_pct"])

    def close_pos(pos: Position, price: float, ts: int, reason: str, frac: float = 1.0) -> None:
        nonlocal equity
        qty = pos.qty * frac
        if qty <= 0:
            return
        notion = pos.entry * qty
        move = price_move(pos.side, pos.entry, price)
        fees = notion * FEE_RATE
        if frac >= 0.999:
            fees += pos.notional * FEE_RATE * (pos.qty / max(pos.qty, 1e-12)) * 0  # entry fee already charged
        pnl = notion * move - fees
        equity += pnl
        pos.qty -= qty
        trades.append(SimTrade(
            STRATEGY_NAME, pos.book, pos.leader, pos.coin, pos.side,
            pos.entry, price, notion, pnl, fees, reason,
            max(0.0, (ts - pos.entry_ms) / 3600000.0),
        ))
        if pos.qty <= 1e-9:
            positions.pop(pos.key(), None)

    def size_for(equity_now: float, risk_pct: float, stop_pct: float, lev: float, reserved: float) -> float:
        risk = equity_now * risk_pct
        notion = risk / max(stop_pct / 100.0, 1e-6)
        notion = min(notion, reserved * lev)
        return notion if notion >= MIN_NOTIONAL else 0.0

    for ev in events:
        ts, coin, action, px, leader = ev["t"], ev["coin"], ev["action"], ev["px"], ev["leader"]
        side = "long" if "long" in action else "short"
        key = (coin, side)
        dd = (peak - equity) / peak if peak > 0 else 0
        if dd >= cfg["drawdown_off_tactical"]:
            tactical_on = False
        risk_scale = 0.5 if dd >= cfg["drawdown_half"] else 1.0
        reg = regime_at(regimes, ts)

        # manage open books on candle path
        for pos in list(positions.values()):
            rows = store.window(pos.coin, max(pos.entry_ms, ts - 6 * 3600000), ts)
            if not rows:
                continue
            last = rows[-1]
            high, low, close = last[2], last[3], last[4]
            mfe = price_move(pos.side, pos.entry, high if pos.side == "long" else low)
            pos.peak_mfe = max(pos.peak_mfe, mfe)
            # MAE stop
            if pos.side == "long":
                mae = (pos.entry - low) / pos.entry * 100
                stop_hit = mae >= pos.stop_pct
                stop_px = pos.entry * (1 - pos.stop_pct / 100)
            else:
                mae = (high - pos.entry) / pos.entry * 100
                stop_hit = mae >= pos.stop_pct
                stop_px = pos.entry * (1 + pos.stop_pct / 100)
            if stop_hit:
                close_pos(pos, stop_px, ts, "mae_stop")
                continue
            if pos.book == "core":
                margin_pnl = price_move(pos.side, pos.entry, close) * pos.lev
                if (not pos.scaled1) and margin_pnl >= cfg["ladder_1"]:
                    close_pos(pos, close, ts, "ladder_100", cfg["ladder_frac"])
                    pos.scaled1 = True
                if pos.key() in positions and (not pos.scaled2) and margin_pnl >= cfg["ladder_2"]:
                    close_pos(pos, close, ts, "ladder_300", cfg["ladder_frac"])
                    pos.scaled2 = True
                # ratchet to breakeven after first ladder
                if pos.scaled1 and price_move(pos.side, pos.entry, close) <= 0:
                    close_pos(pos, close, ts, "ratchet_be")
                    continue

        used = sum(p.notional / p.lev * (p.qty * p.entry / max(p.notional, 1e-9)) for p in positions.values())
        reserve = equity * cfg["reserve_pct"]
        free = max(0.0, equity - used - reserve)
        if equity > 0 and used / equity >= 0.70:
            liq_alerts += 1

        if action.startswith("open_"):
            if leader not in keep_leaders:
                continue
            if key in positions:
                continue
            # classify expected style from this leader's typical hold — use event hint if present
            style = ev.get("style") or "intraday"
            book = "core" if style == "swing" else "tactical"
            if equity < 80 and style != "swing":
                continue
            if equity < 80:
                book = "core"
            if book == "tactical" and not tactical_on:
                continue
            styles = analysis.get(leader, {}).get("styles", {})
            if styles.get(style, {}).get("verdict") == "drop":
                continue
            if book == "core" and sum(1 for p in positions.values() if p.book == "core") >= cfg["max_core"]:
                continue
            if book == "tactical" and sum(1 for p in positions.values() if p.book == "tactical") >= cfg["max_tactical"]:
                continue
            if reg == "bull" and side == "short" and book == "core":
                continue
            if reg == "bear" and side == "long" and book == "core":
                continue
            if reg == "chop" and book == "core":
                continue
            stop_pct = stop_for(leader, style)
            risk = cfg["core_risk_pct"] if book == "core" else cfg["tactical_risk_pct"]
            lev = cfg["core_leverage"] if book == "core" else cfg["tactical_leverage"]
            notion = size_for(equity, risk * risk_scale, stop_pct, lev, free)
            if notion <= 0:
                continue
            heat = sum(p.notional * (p.stop_pct / 100) for p in positions.values()) + notion * (stop_pct / 100)
            if heat > equity * cfg["heat_pct"]:
                continue
            qty = notion / px
            equity -= notion * FEE_RATE  # entry fee
            positions[key] = Position(book, leader, coin, side, px, qty, notion, lev, stop_pct, ts)
        else:
            pos = positions.get(key)
            if pos:
                close_pos(pos, px, ts, "leader_close")
        peak = max(peak, equity)
        if peak > 0:
            max_dd = max(max_dd, (peak - equity) / peak)

    return summarize(STRATEGY_NAME, trades, equity, start_equity, max_dd, liq_alerts)


def summarize(name: str, trades: list[SimTrade], equity: float, start: float, max_dd: float, liq: int) -> dict:
    wins = [t for t in trades if t.pnl > 0]
    losses = [t for t in trades if t.pnl <= 0]
    reasons: dict[str, int] = defaultdict(int)
    coins: dict[str, float] = defaultdict(float)
    for t in trades:
        reasons[t.reason] += 1
        coins[t.coin] += t.pnl
    worst = sorted(coins.items(), key=lambda x: x[1])[:5]
    return {
        "strategy": name,
        "start_equity": start,
        "final_equity": equity,
        "net_pnl": equity - start,
        "ret_pct": (equity / start - 1) * 100 if start else 0,
        "max_dd_pct": max_dd * 100,
        "trades": len(trades),
        "win_rate": 100 * len(wins) / len(trades) if trades else 0,
        "avg_win": sum(t.pnl for t in wins) / len(wins) if wins else 0,
        "avg_loss": sum(t.pnl for t in losses) / len(losses) if losses else 0,
        "fees": sum(t.fees for t in trades),
        "liq_alerts": liq,
        "reasons": dict(reasons),
        "worst_coins": worst,
        "sample_trades": [asdict(t) for t in trades[:20]],
        "all_trades": [asdict(t) for t in trades],
    }


def analyst_review(trades: list[LeaderTrade], analysis: dict, regime: str) -> list[dict]:
    """Best-effort advisory layer. Rule-based v1; NVIDIA/claw402 can replace later."""
    notes = []
    notes.append({
        "role": "analyst_v1",
        "regime": regime,
        "message": (
            f"{STRATEGY_NAME}: market regime is {regime}. "
            "Core book should prefer trend-aligned swings; tactical shorts in a bull regime stay half-size or off."
        ),
    })
    for leader, info in analysis.items():
        if info.get("verdict") == "drop":
            notes.append({
                "role": "analyst_v1",
                "leader": leader,
                "recommend": "drop",
                "confidence": 0.8,
                "message": f"{leader} public-fill net is {info.get('net', 0):.2f}. Do not copy until the record turns positive.",
            })
        else:
            notes.append({
                "role": "analyst_v1",
                "leader": leader,
                "recommend": "keep",
                "confidence": 0.7,
                "message": f"{leader} keeps a positive public-fill net of {info.get('net', 0):.2f}. Copy swings; scalp only if style verdict is copy_exactly.",
            })
    swings = [t for t in trades if t.style == "swing" and t.mfe_pct > 20]
    if swings:
        notes.append({
            "role": "analyst_v1",
            "recommend": "hold_runners",
            "confidence": 0.75,
            "message": (
                f"{len(swings)} swing trades showed MFE > 20%. "
                "Scale out on ladders; do not hard-take-profit the runner. This is the 4000% path."
            ),
        })
    notes.append({
        "role": "nvidia_ready",
        "recommend": "hold_ai_advisory",
        "message": (
            "NVIDIA / claw402 AI is wired as advisory only: classify strategy, "
            "say hold/trim/close, never widen a stop or add size. Enable after this backtest beats current."
        ),
    })
    return notes


def copy_normalized_pnl(events: list[dict], notional: float = 50.0) -> dict:
    """What a $50 mirror would earn if it caught every flatten cycle, ignoring stops."""
    opens: dict[tuple[str, str, str], dict] = {}
    pnls = []
    for e in events:
        k = (e["leader"], e["coin"], e["action"].split("_")[-1])
        if e["action"].startswith("open_"):
            opens[k] = e
        elif k in opens:
            o = opens.pop(k)
            move = price_move(k[2], o["px"], e["px"])
            fees = notional * FEE_RATE * 2
            pnls.append(notional * move - fees)
    wins = [p for p in pnls if p > 0]
    return {
        "n": len(pnls),
        "net": sum(pnls),
        "win_rate": 100 * len(wins) / len(pnls) if pnls else 0,
        "avg": sum(pnls) / len(pnls) if pnls else 0,
    }


def events_from_trades(trades: list[LeaderTrade]) -> list[dict]:
    ev = []
    for t in trades:
        ev.append({"t": t.entry_ms, "coin": t.coin, "action": f"open_{t.side}", "px": t.entry, "leader": t.leader, "style": t.style})
        ev.append({"t": t.exit_ms, "coin": t.coin, "action": f"close_{t.side}", "px": t.exit, "leader": t.leader, "style": t.style})
    ev.sort(key=lambda x: (x["t"], 0 if x["action"].startswith("open") else 1))
    return ev


def write_csv(path: str, rows: list[dict], fields: list[str]) -> None:
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        for r in rows:
            w.writerow({k: r.get(k) for k in fields})


def fmt_block(m: dict) -> str:
    return (
        f"{m['strategy']:18s}  ret {m['ret_pct']:+7.1f}%  pnl ${m['net_pnl']:+8.2f}  "
        f"dd {m['max_dd_pct']:5.1f}%  trades {m['trades']:4d}  win {m['win_rate']:5.1f}%  "
        f"fees ${m['fees']:.2f}  liq-alerts {m['liq_alerts']}"
    )


def main() -> None:
    ensure_data()
    days = 90
    start_ms = utc_ms() - days * 24 * 60 * 60 * 1000
    end_ms = utc_ms()
    print(f"=== {STRATEGY_NAME} ({PLAN_NAME}) ===")
    print("Fetching public Hyperliquid leader fills...")

    all_fills = []
    fills_by_leader: dict[str, list[dict]] = {}
    all_trades: list[LeaderTrade] = []
    for spec in LEADERS:
        print(f"  {spec['name']} {spec['address'][:8]}...")
        path = os.path.join(DATA, f"fills_{spec['name'].replace(' ', '_')}.json")
        if os.path.exists(path):
            with open(path, encoding="utf-8") as f:
                fills = json.load(f)
            print(f"    cached fills={len(fills)}")
        else:
            fills = fetch_user_fills(spec["address"], days=days)
            with open(path, "w", encoding="utf-8") as f:
                json.dump(fills, f)
            print(f"    fills={len(fills)}")
        if not fills:
            fills = hl_info({"type": "userFills", "user": spec["address"]}) or []
            print(f"    fallback userFills={len(fills)}")
        fills_by_leader[spec["name"]] = fills
        trades = reconstruct_trades(spec["name"], spec["address"], fills)
        all_fills.extend(fills)
        all_trades.extend(trades)
        print(f"    reconstructed trades={len(trades)}")

    coins = sorted({t.coin for t in all_trades} | {"BTC", "ETH"})
    print(f"Fetching candles for {len(coins)} coins...")
    store = CandleStore()
    store.load_or_fetch(coins, start_ms, end_ms)

    print("Computing MAE/MFE (holds >= 1h only)...")
    for t in all_trades:
        if t.hold_hours >= 1:
            excursion(t, store)

    analysis = analyze_leaders(all_trades)
    regimes = regime_series(store)
    now_regime = regimes[-1][1] if regimes else "unknown"
    print(f"Current BTC regime: {now_regime}")

    events = copyable_events(fills_by_leader)
    crypto_events = [e for e in events if not e.get("hft")]
    print(f"Copyable flatten events: {len(events)} (crypto-only {len(crypto_events)})")
    print(f"Simulating current vs {STRATEGY_NAME}...")
    mirror_all = copy_normalized_pnl(events)
    mirror_crypto = copy_normalized_pnl(crypto_events)
    current_15 = simulate_current(events, store, CURRENT["start_equity"])
    current_100 = simulate_current(events, store, 100.0)
    current_crypto = simulate_current(crypto_events, store, 100.0)
    bmax = simulate_bmax(crypto_events, store, analysis, regimes, BMAX["start_equity"])
    bmax_15 = simulate_bmax(crypto_events, store, analysis, regimes, 15.0)
    bmax_all = simulate_bmax(events, store, analysis, regimes, BMAX["start_equity"])
    notes = analyst_review(all_trades, analysis, now_regime)

    trade_rows = [asdict(t) for t in all_trades]
    write_csv(os.path.join(DATA, "copy_leader_trades.csv"), trade_rows, list(asdict(all_trades[0]).keys()) if all_trades else [])
    report = {
        "name": STRATEGY_NAME,
        "plan": PLAN_NAME,
        "generated_at": iso(utc_ms()),
        "regime_now": now_regime,
        "leaders": analysis,
        "current_15": {k: v for k, v in current_15.items() if k != "all_trades"},
        "current_100": {k: v for k, v in current_100.items() if k != "all_trades"},
        "current_crypto_100": {k: v for k, v in current_crypto.items() if k != "all_trades"},
        "strategy_b_max_100": {k: v for k, v in bmax.items() if k != "all_trades"},
        "strategy_b_max_15": {k: v for k, v in bmax_15.items() if k != "all_trades"},
        "copyable_events": len(events),
        "crypto_events": len(crypto_events),
        "mirror_50_all": mirror_all,
        "mirror_50_crypto": mirror_crypto,
        "strategy_b_max_all_100": {k: v for k, v in bmax_all.items() if k != "all_trades"},
        "analyst": notes,
        "disclaimer": (
            "Public-fill reconstruction + 1h candles. Not a promise. "
            "Do not change live settings until holdout and dry-run confirm."
        ),
    }
    out = os.path.join(DATA, "strategy_b_max_report.json")
    with open(out, "w", encoding="utf-8") as f:
        json.dump(report, f, indent=2)

    print("\n=== Leader verdicts ===")
    for name, info in analysis.items():
        print(f"  {name:16s} n={info['n']:4d} net={info['net']:+10.2f}  {info['verdict']}")
        for style, s in info.get("styles", {}).items():
            print(
                f"    {style:9s} n={s['n']:3d} wr={s['win_rate']:5.1f}% net={s['net']:+8.2f} "
                f"mae90w={s['mae_p90_winners']:.2f}% giveback={s['avg_giveback_pct']:.2f}% {s['verdict']}"
            )

    print("\n=== Simulation ===")
    print(fmt_block(current_15))
    print(fmt_block(current_100))
    print("current crypto-only @ $100:", fmt_block(current_crypto))
    print("B MAX $15 crypto:", fmt_block(bmax_15))
    print("B MAX $100 crypto:", fmt_block(bmax))
    print("B MAX $100 all flatten:", fmt_block(bmax_all))
    print(f"$50 perfect-mirror all flatten: n={mirror_all['n']} net=${mirror_all['net']:+.2f} wr={mirror_all['win_rate']:.1f}%")
    print(f"$50 perfect-mirror crypto: n={mirror_crypto['n']} net=${mirror_crypto['net']:+.2f} wr={mirror_crypto['win_rate']:.1f}%")
    print("\nAnalyst notes:")
    for n in notes:
        print(f"  - {n.get('message')}")
    print(f"\nWrote {out}")


if __name__ == "__main__":
    main()
