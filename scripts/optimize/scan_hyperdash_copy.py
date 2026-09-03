"""Find Hyperdash copytraders GraphQL query and hit the API."""
from __future__ import annotations

import json
import re
import urllib.error
import urllib.request

UA = {"User-Agent": "Mozilla/5.0", "Accept": "application/json, text/plain, */*"}


def get(url: str) -> bytes:
    req = urllib.request.Request(url, headers=UA)
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.read()


def post(url: str, payload: dict, extra: dict | None = None) -> tuple[int, bytes]:
    headers = dict(UA)
    headers["Content-Type"] = "application/json"
    if extra:
        headers.update(extra)
    req = urllib.request.Request(url, data=json.dumps(payload).encode(), headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read() if e.fp else b""


def main() -> None:
    js = get("https://hyperdash.com/assets/copytraders.lazy-LJWM3i-S.js").decode("utf-8", "replace")
    print("copytraders lazy bytes", len(js))
    hosts = sorted(set(re.findall(r"https://[a-zA-Z0-9.-]+", js)))
    print("hosts", hosts)
    for key in ["query ", "mutation ", "copyScore", "CopyTrader", "graphql", "api.hyperdash", "h.hyperdash"]:
        i = js.find(key)
        print("key", key, i)
        if i >= 0:
            print(js[max(0, i - 60) : i + 280])
            print("---")

    mainjs = get("https://hyperdash.com/assets/main-DImU6lgT.js").decode("utf-8", "replace")
    for key in ["api.hyperdash.com", "h.hyperdash.com", "p.hyperdash.com", "VITE_GRAPHQL", "graphqlUri", "GRAPHQL_URL"]:
        for m in re.finditer(key, mainjs):
            print("main", key, mainjs[m.start() - 40 : m.start() + 160].replace("\n", " "))
            break

    for url in [
        "https://api.hyperdash.com/graphql",
        "https://h.hyperdash.com/graphql",
        "https://p.hyperdash.com/graphql",
        "https://api.hyperdash.com/v1/graphql",
    ]:
        st, body = post(url, {"query": "{ __typename }"})
        print("gql", url, st, body[:200])


if __name__ == "__main__":
    main()
