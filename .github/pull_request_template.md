<!-- The repository owner reviews risk:high pull requests. Branch from the
latest main. Reviews apply AGENTS.md and the docs' rules. -->

## Summary

<!-- What changes and why, in a few sentences. Link the issue(s) this closes. -->

## Risk tier

Declare the highest applicable tier, never lower. A `risk:high` label (a path in `.github/risk-paths.json`, level L1 or L2) means tier 3; name each matched path and say why the change is safe.

Scale: 1 low = docs, tests, comments; 2 medium = behaviour change in code; 3 high = security, data loss, releases or publishing, CI and permissions, or any `risk:high` label.

Risk tier: 1 — <!-- justification -->

## Docs housekeeping

<!-- Delete rows that do not apply. -->

- [ ] New docs started from their category's `TEMPLATE.md`
- [ ] Every new doc indexed in `docs/README.md`
- [ ] Cross-links added in both directions (ADR ↔ design ↔ spec ↔ plan)
- [ ] New significant decision recorded as an ADR first, in this PR
- [ ] New or edited ADRs are fact-only: context, decision and consequences
- [ ] Conformance aliases and canonical symlinks are unchanged; canonical targets are edited instead

## Verification

<!-- Paste evidence for every applicable gate. Delete rows that do not apply. -->

- [ ] `make check` green (host; pnpm directly or with `IN_NSL`)
- [ ] `make vulncheck` at 0 reachable findings, for a change to `go.mod` or `go.sum`
- [ ] Shared vectors pass in Go and TypeScript, for a change to pattern semantics or house geometry
- [ ] Backup round-trip tests updated, for a storage migration
- [ ] No hardware access: tests use the simulated controller and simulated OIDC
- [ ] Revert test for a fix: the new test fails with the fix reverted; command: <!-- name it -->
- [ ] Workflows changed: every action is SHA-pinned with `# vX.Y.Z`, `permissions: {}`, and `persist-credentials: false`
- [ ] Blocking review point fixed: guard added and 1 file added under `.memory/corrections/`
