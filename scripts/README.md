# Operational scripts

This directory holds ad-hoc, single-file Go programs written during live
operation of this project's Bitget/Hyperliquid trading account — things like
stopping a specific set of traders, auditing a close event, or pointing a
leader wallet at a new address. Each file is a standalone `package main` with
its own `func main()`, excluded from normal builds via `//go:build ignore`,
and meant to be run individually with `go run scripts/<name>.go`.

**This is not part of the core deployed service, and it is not covered by
the same testing standard as the rest of the codebase.** These scripts talk
directly to a running instance's API using a signed JWT (minted locally from
`JWT_SECRET`, never committed) and were written for one specific situation
at one specific time — several encode trader names, wallet addresses, or
position sizes that were only ever meaningful during this project's active
trading window.

This project is no longer live-traded (see the root `README.md`). These
scripts are kept for reference — as real examples of the operational
surface the app exposes (`/api/my-traders`, `/api/strategies/:id`,
`/api/traders/:id/start|stop`, and so on) — not as tooling meant to be run
against a live account today.
