"""Introspect Hyperdash GraphQL for copytrader queries."""
from __future__ import annotations

import json
import urllib.request

URL = "https://api.hyperdash.com/graphql"


def gql(query: str, variables: dict | None = None) -> dict:
    payload = {"query": query}
    if variables:
        payload["variables"] = variables
    req = urllib.request.Request(
        URL,
        data=json.dumps(payload).encode(),
        headers={"User-Agent": "Mozilla/5.0", "Content-Type": "application/json", "Accept": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read().decode())


def main() -> None:
    q = """
    query IntrospectQuery {
      __schema {
        queryType { name }
        types {
          name
          kind
          fields {
            name
            args { name type { name kind ofType { name kind ofType { name } } } }
          }
        }
      }
    }
    """
    data = gql(q)
    if "errors" in data:
        print(json.dumps(data["errors"], indent=2)[:2000])
        return
    types = data["data"]["__schema"]["types"]
    interesting = []
    for t in types:
        name = (t.get("name") or "").lower()
        if any(x in name for x in ("copy", "trader", "wallet", "leader", "explore", "score")):
            interesting.append(t)
    print("interesting types", len(interesting))
    for t in interesting:
        print("TYPE", t["kind"], t["name"])
        for f in (t.get("fields") or [])[:40]:
            print("  ", f["name"], [a["name"] for a in (f.get("args") or [])])

    # query root fields
    qname = data["data"]["__schema"]["queryType"]["name"]
    root = next(t for t in types if t["name"] == qname)
    print("ROOT", qname)
    for f in root.get("fields") or []:
        n = f["name"].lower()
        if any(x in n for x in ("copy", "trader", "wallet", "leader", "explore", "score")):
            print(" Q", f["name"], [a["name"] for a in (f.get("args") or [])])


if __name__ == "__main__":
    main()
