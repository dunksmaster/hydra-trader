"""Fetch Hyperdash copytrading page assets and probe APIs (read-only)."""
from __future__ import annotations

import json
import re
import urllib.error
import urllib.request

UA = {"User-Agent": "Mozilla/5.0", "Accept": "*/*"}


def get(url: str, timeout: int = 20) -> tuple[int, str, bytes]:
    req = urllib.request.Request(url, headers=UA)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, r.getheader("content-type") or "", r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.headers.get("content-type") or "", e.read() if e.fp else b""


def main() -> None:
    status, ctype, body = get("https://hyperdash.com/copytrading")
    print("copytrading", status, ctype, "bytes", len(body))
    html = body.decode("utf-8", "replace")
    open("scripts/optimize/data/hyperdash_copytrading.html", "w", encoding="utf-8").write(html)
    scripts = re.findall(r'(?:src|href)="([^"]+)"', html)
    print("assets", [s for s in scripts if "http" in s or "_next" in s or ".js" in s][:50])
    for pat in ["_next", "graphql", "supabase", "convex", "trpc", "copyScore", "copytrader"]:
        print(pat, html.lower().count(pat.lower()))
    addrs = sorted(set(re.findall(r"0x[a-fA-F0-9]{40}", html)))
    print("addrs_in_html", addrs)
    apis = sorted(set(re.findall(r"https?://[a-zA-Z0-9._:-]+/[^\"'\s]*", html)))
    print("urls_sample")
    for u in apis[:40]:
        if "api" in u.lower() or "graphql" in u.lower() or "trader" in u.lower():
            print(" ", u)

    status2, _, body2 = get("https://hyperdash.com/explore/copytraders")
    html2 = body2.decode("utf-8", "replace")
    print("copytraders", status2, "bytes", len(body2), "addrs", sorted(set(re.findall(r"0x[a-fA-F0-9]{40}", html2))))
    scripts2 = re.findall(r'src="([^"]+)"', html2)
    print("scripts2", scripts2[:30])

    candidates = [
        "https://hyperdash.com/api/trader-explore/copytraders",
        "https://hyperdash.com/api/explore/copytraders?sort=copy_score",
        "https://hyperdash.com/api/v1/copytraders",
        "https://stats-data.hyperliquid.xyz/Mainnet/leaderboard",
    ]
    for u in candidates:
        st, ct, b = get(u)
        print("probe", st, ct, u, b[:160])


if __name__ == "__main__":
    main()
