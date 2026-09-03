"""Query Hyperdash copytraders and verify last-24h Hyperliquid fills."""
from __future__ import annotations

import json
import time
import urllib.request
from datetime import datetime, timezone

GQL = "https://api.hyperdash.com/graphql"
HL = "https://api.hyperliquid.xyz/info"

EXISTING = {
    "0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf",
    "0xdebbea84972174f44778a00521b1b5faa663abbb",
    "0x8a0cd16a004e21e04936a0a01c6f9a49ff937914",
    "0x6a02aedceac5a6813d960e4dae1910d9c458e77c",
    "0x6859da14835424957a1e6b397d8026b1d9ff7e1e",
}

QUERY = """
query ExploreTraders($page: Int, $pageSize: Int, $timeframe: TraderTimeframe!, $sortBy: TraderSortInput, $filters: TraderFilterInput) {
  exploreTraders(page: $page, pageSize: $pageSize, timeframe: $timeframe, sortBy: $sortBy, filters: $filters) {
    data {
      address
      label
      displayName
      pnl
      perpsEquity
      winrate
      totalTrades
      totalWinningTrades
      totalLosingTrades
      sharpe
      drawdown
      copyScore
      tag
    }
    pagination { page pageSize totalItems totalPages }
  }
}
"""


def gql(query: str, variables: dict) -> dict:
    req = urllib.request.Request(
        GQL,
        data=json.dumps({"query": query, "variables": variables}).encode(),
        headers={"User-Agent": "Mozilla/5.0", "Content-Type": "application/json", "Accept": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read().decode())


def hl(payload: dict):
    req = urllib.request.Request(
        HL,
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json", "User-Agent": "Mozilla/5.0"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read().decode())


def last_24h_fills(addr: str) -> tuple[int, int | None]:
    start = int(time.time() * 1000) - 24 * 60 * 60 * 1000
    try:
        raw = hl({"type": "userFillsByTime", "user": addr, "startTime": start})
    except Exception:
        raw = hl({"type": "userFills", "user": addr})
        if isinstance(raw, list):
            raw = [f for f in raw if int(f.get("time") or 0) >= start]
    if not isinstance(raw, list):
        return 0, None
    last = max((int(f.get("time") or 0) for f in raw), default=0)
    return len(raw), last or None


def main() -> None:
    attempts = [
        {"timeframe": "seven_days", "sortBy": {"field": "copyScore", "order": "desc"}},
        {"timeframe": "seven_days", "sortBy": {"field": "copy_score", "order": "desc"}},
        {"timeframe": "thirty_days", "sortBy": {"field": "copyScore", "order": "desc"}},
        {"timeframe": "one_day", "sortBy": {"field": "copyScore", "order": "desc"}},
    ]
    data = None
    for vars0 in attempts:
        vars0 = dict(vars0)
        vars0.update({"page": 1, "pageSize": 25, "filters": {}})
        print("try", vars0)
        resp = gql(QUERY, vars0)
        if resp.get("errors"):
            print(" errors", resp["errors"][0].get("message"))
            continue
        data = resp
        break
    if not data:
        raise SystemExit("exploreTraders query failed")
    rows = data["data"]["exploreTraders"]["data"]
    print("got", len(rows), "traders")
    out = []
    for row in rows:
        addr = (row.get("address") or "").lower()
        n, last = last_24h_fills(addr)
        rec = {
            **row,
            "fills_24h": n,
            "last_fill": datetime.fromtimestamp(last / 1000, tz=timezone.utc).isoformat() if last else None,
            "already_copied": addr in EXISTING,
        }
        out.append(rec)
        print(
            f"{row.get('copyScore')} wr={row.get('winrate')} pnl={row.get('pnl')} eq={row.get('perpsEquity')} "
            f"trades={row.get('totalTrades')} 24h={n} {addr} {row.get('displayName') or row.get('label')}"
        )
        time.sleep(0.12)

    picked = []
    for rec in out:
        if rec["already_copied"]:
            continue
        if rec["fills_24h"] <= 0:
            continue
        wr = rec.get("winrate") or 0
        cs = rec.get("copyScore") or 0
        if cs < 70 and wr < 0.5:
            continue
        picked.append(rec)
        if len(picked) >= 3:
            break
    report = {"source": "https://hyperdash.com/copytrading", "candidates": picked, "scanned": out[:15]}
    path = "scripts/optimize/data/hyperdash_candidates.json"
    with open(path, "w", encoding="utf-8") as f:
        json.dump(report, f, indent=2)
    print("WROTE", path)
    print(json.dumps(picked, indent=2))


if __name__ == "__main__":
    main()
