from __future__ import annotations
import urllib.request
URL = "https://hyperdash.com/assets/main-DImU6lgT.js"
js = urllib.request.urlopen(urllib.request.Request(URL, headers={"User-Agent": "Mozilla/5.0"}), timeout=30).read().decode("utf-8", "replace")
for key in ['field:"copyScore"', "field:'copyScore'", "COPY_SCORE", "CopyScore", "sortBy:", "TraderSort"]:
    i = 0
    n = 0
    while n < 8:
        j = js.find(key, i)
        if j < 0:
            break
        print("====", key, j)
        print(js[j - 80 : j + 160])
        i = j + 1
        n += 1
