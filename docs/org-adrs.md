# Organization ADRs binding Trimlights

Trimlights follows these decisions from
[frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr), the
team's organization standard:

- [ADR-0071](https://github.com/frostyard/core/blob/main/docs/adr/0071-hold-every-code-repository-to-one-baseline.md):
  the repository baseline (core's `docs/specs/repository-baseline.md`, rows B1
  to B17); `internal/workflowcheck` checks the files of rows B1, B2, B5, B6 and
  B13. Row B9 (a documentation site) does not apply: Trimlights serves one
  household's private deployment, and nobody else installs it.
  [ADR-0073](https://github.com/frostyard/core/blob/main/docs/adr/0073-let-ci-run-the-make-ci-targets-as-separate-jobs.md):
  CI can run the targets of `make ci` as separate jobs; the `check` job of
  `.github/workflows/ci.yml` runs `make ci` in 1 job.
- [ADR-0018](https://github.com/frostyard/core/blob/main/docs/adr/0018-org-wide-agent-instruction-and-knowledge-surfaces.md),
  [ADR-0029](https://github.com/frostyard/core/blob/main/docs/adr/0029-acmm-conformance-via-canonical-aliases.md):
  `AGENTS.md` is canonical. `CLAUDE.md`, `GEMINI.md`, `.cursorrules` and
  `.github/copilot-instructions.md` are relative symlinks to it, and
  `.claude/skills` is a symlink to `.agents/skills/`
  ([ADR-0002](adr/0002-agent-portable-instruction-surface.md)).
  `.memory/corrections.jsonl` is the corrections inbox.
- [ADR-0019](https://github.com/frostyard/core/blob/main/docs/adr/0019-governance-as-code-and-risk-tiers.md),
  [ADR-0068](https://github.com/frostyard/core/blob/main/docs/adr/0068-label-pull-requests-that-touch-high-risk-paths.md):
  risk tiers in the PR template. `.github/risk-paths.json` holds the high-risk
  paths; its PR triage caller `.github/workflows/risk-paths.yml` labels a PR
  with its level and with `risk:high` at L1 and L2.
- [ADR-0076](https://github.com/frostyard/core/blob/main/docs/adr/0076-require-status-checks-on-every-default-branch.md):
  every default branch requires a status check, so a red or a missing CI
  blocks a merge.
- [ADR-0077](https://github.com/frostyard/core/blob/main/docs/adr/0077-bind-ready-for-human-to-the-head-commit.md):
  `ready-for-human` is bound to the head commit. The PR triage caller removes
  it when the head moves after the label, or when CI at the label is not green.
- [ADR-0078](https://github.com/frostyard/core/blob/main/docs/adr/0078-sort-pull-requests-by-levels-l1-to-l4.md):
  a PR carries 1 level label from `level:L1` to `level:L4`, and
  `.github/risk-paths.json` is schema version 2 (a level for each glob, and the
  default `L3`). It amends ADR-0068 in part.
- [ADR-0021](https://github.com/frostyard/core/blob/main/docs/adr/0021-sha-pinned-actions-and-least-privilege-ci.md):
  CI actions are pinned to full commit SHAs, with least-privilege permissions
  ([ADR-0015](adr/0015-versioned-attested-images.md); `internal/workflowcheck`).
- [ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md):
  one `docs/` tree in the four-category shape.
- [ADR-0038](https://github.com/frostyard/core/blob/main/docs/adr/0038-scope-the-test-name-filter-to-chairlift.md):
  `make ci` is the canonical gate, and tests run without a name filter.
- [ADR-0040](https://github.com/frostyard/core/blob/main/docs/adr/0040-publish-the-repository-settings-contract.md):
  private vulnerability reporting is the security channel (`SECURITY.md`).
- [ADR-0043](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md),
  [ADR-0044](https://github.com/frostyard/core/blob/main/docs/adr/0044-expose-the-make-gate-triad-in-every-repository.md):
  tools are pinned in `mise.toml` with `mise.lock`, and the gates are
  `make verify`, `check` and `ci`
  ([ADR-0016](adr/0016-adopt-the-frostyard-make-gate.md)).
- [ADR-0012](https://github.com/frostyard/core/blob/main/docs/adr/0012-svu-versioning-and-rolling-dev-prerelease.md),
  [ADR-0053](https://github.com/frostyard/core/blob/main/docs/adr/0053-name-the-svu-config-svu-yml.md):
  svu versions the `vX.Y.Z` tags with `.svu.yml`; `make bump` tags
  `svu next` ([ADR-0015](adr/0015-versioned-attested-images.md)).
- [ADR-0072](https://github.com/frostyard/core/blob/main/docs/adr/0072-write-every-readme-to-one-standard.md):
  the root `README.md` follows core's README spec for the kind `service`.
