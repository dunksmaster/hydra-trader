# Hydra Trader — Publish Checklist

**Fork of:** [NoFxAiOS/nofx](https://github.com/NoFxAiOS/nofx) (AGPL-3.0)
**This checklist tracks readiness for a standalone public release under this fork's own identity.**

---

## What this fork adds beyond upstream

See [`FORK-ARCHITECTURE.md`](FORK-ARCHITECTURE.md) for diagrams. Summary:

1. **Hyperliquid multi-leader copy-trading engine** — `trader/auto_trader_copy.go`,
   `trader/auto_trader_copy_fills.go`, `trader/copy_leader_watcher.go`. Leader-fill
   mirroring with `tid`-based deduplication, a two-tier position cap (per-leader +
   wallet-wide, wallet checked first), and an overflow queue for skipped opens rather
   than silent drops.
2. **Telegram system-alert wiring** — `events/alert.go` + `trader/runtime_health.go`,
   wired at 5 points in the trading loop (AI wallet empty, quota exhausted,
   rate-limited, Safe Mode on/off). Upstream had none of this.
3. **Bitget UTA (Unified Trading Account) v3 support** — `trader/bitget/trader_uta.go`.
4. **Operational scripts** (`scripts/`) — ad-hoc live-account tooling, not part of the
   core deployed service.

---

## Known issues, stated honestly

- **All order execution uses IOC (Immediate-Or-Cancel) exclusively** — every open,
  close, stop-loss, and take-profit pays taker fees. No maker-fee (GTC) path exists
  for copy-trading or AI-decision opens.
- **`GetPositions()` staleness is unconfirmed** as a possible cause of rapid same-symbol
  re-opens on high-frequency leaders — raised, not yet resolved.
- **No backtesting anywhere in the system** — every strategy/risk change has been
  validated live, with real money, not before deployment.

---

## License obligations

**AGPL-3.0, inherited from upstream — not a license this fork can change.** The
network-service clause means running a modified version of this code as a live service
to users obligates making the modified source available to those users. Upstream
(NoFxAiOS) actively enforces this — see their real enforcement action against another
company for a same-behavior rewrite in a different language:
`docs/legal/AGPL-VIOLATION-REPORT-ChainOpera-EN.md` in this repo. Their own stated
position: **"even a rewrite still constitutes an AGPL violation"** if derived from
NOFX's design.

---

## Checklist

- [x] **Security review of application code** (api/, crypto/, telegram/agent/, trader/,
      mcp/, store/) — no high-confidence vulnerabilities found. Ownership scoping,
      SQL parameterization, secrets encryption, SSRF protection, and JWT handling all
      checked and sound.
- [x] **Secrets scan clean** — tracked files, full git history, and session notes
      checked for Bitget/Hyperliquid/NVIDIA key patterns. None found. `.gitignore`
      correctly covers `.env`, `secrets/`, `*.key`, `*.pem`, `rsa_key*`.
- [x] **CI/CD workflow spot-check** — `docker-build.yml` uses `pull_request` (not the
      higher-risk `pull_request_target`), and gates image pushes/secrets behind
      `github.event_name != 'pull_request'`, so fork PRs never get registry credentials
      or push rights.
- [ ] **Uncommitted local changes need to be committed** — a large amount of real
      application code (trader/, telegram/, api/, store/, kernel/, mcp/ core logic,
      plus operational scripts) exists only in an uncommitted working tree, not in git
      history. A public repo needs what's running and what's published to match.
- [ ] **Decide what happens to `scripts/` ops tooling** — publish as-is with a
      "not production-supported" note, or exclude from the public release.
- [ ] **Confirm AGPL-3.0 stays the license**, and that the published `LICENSE` file and
      README badge are accurate.
