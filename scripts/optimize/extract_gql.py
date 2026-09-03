"""Extract GraphQL operation strings mentioning copyScore / copytraders."""
from __future__ import annotations

import re
import urllib.request

URLS = [
    "https://hyperdash.com/assets/main-DImU6lgT.js",
    "https://hyperdash.com/assets/ExploreV2Page-DjJodwYQ.js",
    "https://hyperdash.com/assets/copytraders.lazy-LJWM3i-S.js",
    "https://hyperdash.com/assets/global.lazy-DBzruNV_.js",
]


def get(url: str) -> str:
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.read().decode("utf-8", "replace")


def main() -> None:
    for url in URLS:
        print("===", url)
        try:
            js = get(url)
        except Exception as e:
            print("fail", e)
            continue
        print("bytes", len(js))
        for pat in [r"query [A-Za-z0-9_]+", r"mutation [A-Za-z0-9_]+"]:
            names = sorted(set(re.findall(pat, js)))
            if names:
                print(pat, names[:40])
        # string literals containing copyScore
        for m in re.finditer(r".{0,80}copyScore.{0,120}", js):
            s = m.group(0)
            if "query" in s or "fragment" in s or "{" in s:
                print("CTX", s[:200])
        # gql tagged templates
        for m in re.finditer(r"gql`[^`]{20,800}`", js):
            if "copy" in m.group(0).lower() or "trader" in m.group(0).lower():
                print("GQL", m.group(0)[:400])
                print("---")


if __name__ == "__main__":
    main()
