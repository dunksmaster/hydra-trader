from __future__ import annotations
import urllib.request

URL = "https://hyperdash.com/assets/main-DImU6lgT.js"
js = urllib.request.urlopen(urllib.request.Request(URL, headers={"User-Agent": "Mozilla/5.0"}), timeout=30).read().decode("utf-8", "replace")
for key in ["TraderTimeframe", "TraderSortInput", "TraderSortField", "one_day", "seven_days", "copyScore"]:
    i = js.find(key)
    print("====", key, i)
    if i >= 0:
        print(js[i : i + 400])
