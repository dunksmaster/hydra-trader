"""Extract ExploreTraders GraphQL document from Hyperdash main bundle."""
from __future__ import annotations

import json
import re
import urllib.request

URL = "https://hyperdash.com/assets/main-DImU6lgT.js"


def main() -> None:
    req = urllib.request.Request(URL, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=30) as r:
        js = r.read().decode("utf-8", "replace")
    i = js.find("query ExploreTraders")
    print("idx", i)
    print(js[i : i + 2500])
    print("==== GetTraderCard")
    j = js.find("query GetTraderCard")
    print(js[j : j + 1500])


if __name__ == "__main__":
    main()
