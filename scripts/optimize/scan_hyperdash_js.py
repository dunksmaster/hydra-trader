"""Scan Hyperdash JS bundles for GraphQL/API endpoints and copytrader queries."""
from __future__ import annotations

import re
import urllib.request

UA = {"User-Agent": "Mozilla/5.0"}
ASSETS = [
    "https://hyperdash.com/assets/main-DImU6lgT.js",
    "https://hyperdash.com/assets/vendor-apollo-D-XYotQk.js",
    "https://hyperdash.com/assets/vendor-query-z1Q1YFeI.js",
]


def get(url: str) -> str:
    req = urllib.request.Request(url, headers=UA)
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.read().decode("utf-8", "replace")


def main() -> None:
    for url in ASSETS:
        print("===", url)
        js = get(url)
        print("bytes", len(js))
        for pat in [
            r"https://[a-zA-Z0-9._/-]+graphql[a-zA-Z0-9._/-]*",
            r"https://[a-zA-Z0-9._/-]+api[a-zA-Z0-9._/-]*",
            r"copyTrader[sA-Za-z]*",
            r"copyScore",
            r"CopyScore",
            r"hasura",
            r"apollo",
        ]:
            hits = re.findall(pat, js)
            if hits:
                uniq = sorted(set(hits))
                print(pat, "count", len(hits), "uniq", uniq[:20])
        # nearby copytrading strings
        for m in re.finditer(r".{40}copytrad.{40}", js, re.I):
            print("ctx", m.group(0)[:120])
            break


if __name__ == "__main__":
    main()
