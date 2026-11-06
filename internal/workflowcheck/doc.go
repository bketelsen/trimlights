// Package workflowcheck holds the tests that keep the repository's own
// files in line with frostyard/core: the GitHub Actions workflows within
// ADR-0021 (external actions pinned to a full commit SHA, permissions
// declared at the top, and checkouts that do not keep the token), and the
// files of the repository baseline (ADR-0071, rows B1, B2, B6 and B13): the
// instruction symlinks, the corrections inbox, the risk-path list and the
// Dependabot entries. The checks run in `make verify`, so a pull request that
// departs from the baseline fails the required `check` job.
//
// The workflows and .github/dependabot.yml are read with a small scanner of
// keys, scalar values and sequence items, not with a YAML parser: the
// repository's YAML module is an indirect dependency of the CLI's chain, and a
// direct import in a test moves it into the direct require block of go.mod,
// which `make tidy-check` reports as a change.
package workflowcheck
