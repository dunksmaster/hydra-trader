"""Deeper scan of Hyperdash main bundle for network endpoints."""
from __future__ import annotations

import re
import urllib.request

URL = "https://hyperdash.com/assets/main-DImU6lgT.js"


def main() -> None:
    req = urllib.request.Request(URL, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=30) as r:
        js = r.read().decode("utf-8", "replace")
    print("len", len(js))
    hosts = sorted(set(re.findall(r"https://[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}", js)))
    print("hosts")
    for h in hosts:
        if any(x in h.lower() for x in ("hyper", "dash", "gql", "graph", "api", "hasura", "supabase", "aws")):
            print(" ", h)
    print("all hosts count", len(hosts))
    for h in hosts:
        print(" ", h)
    for key in ["uri:", "graphql", "copyScore", "lastTrade", "GetCopy", "copytraders"]:
        idx = js.lower().find(key.lower())
        print("first", key, idx)
        if idx >= 0:
            print(js[max(0, idx - 80) : idx + 200].replace("\n", " "))


if __name__ == "__main__":
    main()
