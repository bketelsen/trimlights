package workflowcheck

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// skipped reports whether the repository walk leaves the directory name out:
// version control, the frontend's dependencies, and build output.
func skipped(name string) bool {
	switch name {
	case ".git", "node_modules", "build", "dist", "target":
		return true
	}
	return false
}

// repoFiles returns every file of the repository as a path from its root with
// forward slashes.
func repoFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipped(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestInstructionAliases guards row B1 of frostyard/core
// docs/specs/repository-baseline.md: AGENTS.md is the one instruction file,
// and every other agent entry point is a relative symlink to it.
func TestInstructionAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows checkout has no symlinks")
	}
	agents := filepath.Join("..", "..", "AGENTS.md")
	fi, err := os.Lstat(agents)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() {
		t.Errorf("AGENTS.md is not a regular file (mode %s); it is the one instruction file", fi.Mode())
	}
	want, err := filepath.EvalSymlinks(agents)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"CLAUDE.md",
		"GEMINI.md",
		".cursorrules",
		filepath.Join(".github", "copilot-instructions.md"),
	} {
		if got, ok := relativeSymlink(t, name); ok && got != want {
			t.Errorf("%s resolves to %q, want %q", name, got, want)
		}
	}

	name := filepath.Join(".claude", "skills")
	got, ok := relativeSymlink(t, name)
	wantSkills, err := filepath.EvalSymlinks(filepath.Join("..", "..", ".agents", "skills"))
	if err != nil {
		t.Fatalf(".agents/skills: %v", err)
	}
	if ok && got != wantSkills {
		t.Errorf("%s resolves to %q, want %q", name, got, wantSkills)
	}
	if fi, err := os.Stat(filepath.Join("..", "..", name)); err != nil {
		t.Errorf("%s: %v", name, err)
	} else if !fi.IsDir() {
		t.Errorf("%s resolves to a file, want the directory .agents/skills", name)
	}
}

// relativeSymlink reports whether name is a symlink whose target is relative,
// and returns the path it resolves to.
func relativeSymlink(t *testing.T, name string) (string, bool) {
	t.Helper()
	link := filepath.Join("..", "..", name)
	fi, err := os.Lstat(link)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return "", false
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is not a symlink; it must be a relative symlink to AGENTS.md, so that 1 edit reaches every agent", name)
		return "", false
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return "", false
	}
	if filepath.IsAbs(target) {
		t.Errorf("%s links to the absolute path %q; it must be relative", name, target)
		return "", false
	}
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return "", false
	}
	return got, true
}

// correctionsMember names the 5 members a line of .memory/corrections.jsonl
// carries (row B2).
var correctionsMember = []string{"date", "scope", "correction", "evidence", "promoted_to"}

// datePattern is the form of a correction's date.
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// correctionProblems returns the problems of 1 line of
// .memory/corrections.jsonl, after frostyard/core scripts/check-corrections.mjs
// (row B2); no problem means a valid line. promoted_to may be empty, the way an
// unpromoted correction has it.
func correctionProblems(line string) []string {
	if strings.TrimSpace(line) == "" {
		return []string{"blank line"}
	}
	var value any
	if err := json.Unmarshal([]byte(line), &value); err != nil {
		return []string{fmt.Sprintf("invalid JSON: %v", err)}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return []string{"not a plain object"}
	}

	var problems, missing, unknown []string
	for _, name := range correctionsMember {
		if _, ok := obj[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range obj {
		if !slices.Contains(correctionsMember, name) {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	if len(missing) > 0 {
		problems = append(problems, "missing key(s): "+strings.Join(missing, ", "))
	}
	if len(unknown) > 0 {
		problems = append(problems, "unknown key(s): "+strings.Join(unknown, ", "))
	}
	if err := duplicateMembers(line); err != nil {
		problems = append(problems, err.Error())
	}
	for _, name := range correctionsMember {
		v, ok := obj[name]
		if !ok {
			continue
		}
		if _, ok := v.(string); !ok {
			problems = append(problems, "value for "+name+" is not a string")
		}
	}
	if s, ok := obj["date"].(string); ok {
		if !datePattern.MatchString(s) {
			problems = append(problems, "invalid date: "+s)
		} else if _, err := time.Parse("2006-01-02", s); err != nil {
			problems = append(problems, "invalid date: "+s)
		}
	}
	for _, name := range []string{"scope", "correction", "evidence"} {
		if s, ok := obj[name].(string); ok && strings.TrimSpace(s) == "" {
			problems = append(problems, name+" is empty")
		}
	}
	return problems
}

// duplicateMembers returns an error that names the first object member that its
// object carries twice, at any depth, and nil when every member appears once.
// encoding/json keeps the last value of a repeated member, so the names are read
// from the text; frostyard/core's parser rejects a duplicate key at any level
// (docs/specs/risk-paths.md), and its corrections script does the same.
func duplicateMembers(text string) error {
	if where, ok := findDuplicate(json.NewDecoder(strings.NewReader(text)), ""); ok {
		return fmt.Errorf("the member %q appears twice; a duplicate key is not allowed", where)
	}
	return nil
}

// findDuplicate reads 1 JSON value from dec and returns the path of the first
// object member that its object carries twice, with whether there is 1; at is
// the path of the value, such as paths[3], and it is empty at the root.
func findDuplicate(dec *json.Decoder, at string) (string, bool) {
	tok, err := dec.Token()
	if err != nil {
		return "", false
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return "", false
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return "", false
			}
			name, ok := tok.(string)
			if !ok {
				return "", false
			}
			where := name
			if at != "" {
				where = at + "." + name
			}
			if seen[name] {
				return where, true
			}
			seen[name] = true
			if found, ok := findDuplicate(dec, where); ok {
				return found, true
			}
		}
		if _, err := dec.Token(); err != nil {
			return "", false
		}
	case '[':
		for i := 0; dec.More(); i++ {
			if found, ok := findDuplicate(dec, fmt.Sprintf("%s[%d]", at, i)); ok {
				return found, true
			}
		}
		if _, err := dec.Token(); err != nil {
			return "", false
		}
	}
	return "", false
}

// TestCorrectionsInbox guards row B2 of frostyard/core
// docs/specs/repository-baseline.md: .memory/corrections.jsonl holds one JSON
// object per line, with exactly the 5 fields of a correction. An empty file is
// valid: a repository whose reviews have found no blocking point yet has
// recorded no correction, and the first line arrives with the first guard.
func TestCorrectionsInbox(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".memory", "corrections.jsonl"))
	if err != nil {
		t.Fatalf(".memory/corrections.jsonl: %v", err)
	}
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(string(data), "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	}
	for i, line := range lines {
		for _, problem := range correctionProblems(line) {
			t.Errorf("line %d: %s", i+1, problem)
		}
	}
	t.Logf("%d corrections lines", len(lines))
}

// TestCorrectionProblems checks the line rules of row B2 on the cases
// frostyard/core scripts/check-corrections.mjs names.
func TestCorrectionProblems(t *testing.T) {
	const valid = `{"date": "2026-11-05", "scope": "socket", "correction": "c", "evidence": "e", "promoted_to": ""}`
	for _, tc := range []struct {
		name  string
		line  string
		valid bool
	}{
		{"unpromoted", valid, true},
		{"promoted", `{"date": "2026-11-05", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": "a_test.go"}`, true},
		{"blank scope", `{"date": "2026-11-05", "scope": "  ", "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"month 13", `{"date": "2026-13-99", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"february 30", `{"date": "2026-02-30", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"number value", `{"date": "2026-11-05", "scope": 1, "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"missing member", `{"date": "2026-11-05", "scope": "s", "correction": "c", "evidence": "e"}`, false},
		{"unknown member", `{"date": "2026-11-05", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": "", "extra": "x"}`, false},
		{"duplicate member", `{"date": "2026-11-05", "date": "2026-11-05", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"not an object", `["2026-11-05"]`, false},
		{"blank line", "  ", false},
	} {
		problems := correctionProblems(tc.line)
		switch {
		case tc.valid && len(problems) > 0:
			t.Errorf("%s: correctionProblems(%s) = %v, want none", tc.name, tc.line, problems)
		case !tc.valid && len(problems) == 0:
			t.Errorf("%s: correctionProblems(%s) = none, want at least 1", tc.name, tc.line)
		}
	}
}

// TestDuplicateMembers checks the duplicate-key walk on the shapes a risk list
// and a correction line can hold: frostyard/core's parser rejects a duplicate
// key at any depth, and encoding/json keeps the last value of a repeated member.
func TestDuplicateMembers(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string // the member that appears twice, empty when the text is valid
	}{
		{"valid list", `{"schema_version": 1, "paths": [{"glob": "a", "why": "governance: b"}]}`, ""},
		{"duplicate schema_version", `{"schema_version": 1, "schema_version": 1, "paths": []}`, "schema_version"},
		{"duplicate paths", `{"schema_version": 1, "paths": [], "paths": []}`, "paths"},
		{"duplicate glob of an entry", `{"schema_version": 1, "paths": [{"glob": "a", "glob": "b", "why": "governance: c"}]}`, "paths[0].glob"},
		{"duplicate why of an entry", `{"schema_version": 1, "paths": [{"glob": "a", "why": "governance: b", "why": "governance: c"}]}`, "paths[0].why"},
		{"duplicate member of a correction", `{"date": "2026-11-05", "date": "2026-11-05"}`, "date"},
		{"valid correction line", `{"date": "2026-11-05", "scope": "s"}`, ""},
		{"a scalar has no member", `1`, ""},
	} {
		err := duplicateMembers(tc.text)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: duplicateMembers(%s) = %v, want no error", tc.name, tc.text, err)
		case tc.want != "" && err == nil:
			t.Errorf("%s: duplicateMembers(%s) = nil, want an error that names %q", tc.name, tc.text, tc.want)
		case tc.want != "" && !strings.Contains(err.Error(), `"`+tc.want+`"`):
			t.Errorf("%s: duplicateMembers(%s) = %v, want it to name %q", tc.name, tc.text, err, tc.want)
		}
	}
}

// riskPath is 1 entry of .github/risk-paths.json.
type riskPath struct {
	Glob string `json:"glob"`
	Why  string `json:"why"`
}

// riskPathFile is the whole of .github/risk-paths.json.
type riskPathFile struct {
	SchemaVersion int        `json:"schema_version"`
	Paths         []riskPath `json:"paths"`
}

// checkMembers reports each member of the JSON object data that allowed does
// not name: encoding/json drops an unknown member, and the schema of
// .github/risk-paths.json names all of them.
func checkMembers(t *testing.T, where string, data []byte, allowed ...string) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Errorf("%s: %v", where, err)
		return
	}
	var unknown []string
	for name := range obj {
		if !slices.Contains(allowed, name) {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	for _, name := range unknown {
		t.Errorf("%s: unknown member %q", where, name)
	}
}

// readRiskPaths returns .github/risk-paths.json, and fails on a member that the
// schema does not name, or on a member that appears twice.
func readRiskPaths(t *testing.T) riskPathFile {
	t.Helper()
	const name = ".github/risk-paths.json"
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "risk-paths.json"))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := duplicateMembers(string(data)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	checkMembers(t, name, data, "schema_version", "paths")
	var doc struct {
		Paths []json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for i, entry := range doc.Paths {
		checkMembers(t, fmt.Sprintf("%s: entry %d", name, i), entry, "glob", "why")
	}
	var cfg riskPathFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return cfg
}

// riskCategories are the categories of frostyard/core docs/specs/risk-paths.md
// (section B); each why starts with 1 of them and a colon.
var riskCategories = []string{
	"boot chain",
	"signing and keys",
	"update verification",
	"auth",
	"publication",
	"secrets and credentials",
	"rulesets and settings",
	"destructive operations",
	"governance",
}

// whyCategory reports whether why starts with 1 of riskCategories and a colon.
func whyCategory(why string) bool {
	category, _, ok := strings.Cut(why, ":")
	return ok && slices.Contains(riskCategories, category)
}

// governedPaths are the release, secrets, auth, destructive and governance
// paths of this repository; .github/risk-paths.json must list each of them.
var governedPaths = []string{
	".github/risk-paths.json",
	".github/workflows/ci.yml",
	".github/workflows/risk-paths.yml",
	"Makefile",
	"mise.toml",
	"mise.lock",
	".golangci.yml",
	".svu.yml",
	"Containerfile",
	".dockerignore",
	"deploy/oidc.example.json",
	"deploy/env.example",
	"deploy/compose.example.yaml",
	"deploy/Caddyfile.example",
	"internal/auth/auth.go",
	"internal/service/http.go",
	"internal/service/house.go",
	"cmd/trimlight/service.go",
	"cmd/trimlight/main.go",
	"cmd/trimlight/playback.go",
	"internal/storage/auth.go",
	"internal/storage/permissions.go",
	"internal/storage/store.go",
	"internal/storage/house.go",
	"internal/storage/calendar_runtime.go",
	"internal/storage/calendar_handoff.go",
	"internal/backup/backup.go",
	"cmd/trimlight/backup.go",
	"internal/trimlight/client.go",
	"internal/trimlight/control.go",
	"internal/trimlight/playback.go",
	"internal/trimlight/protocol.go",
	"internal/service/worker.go",
	"internal/service/controls.go",
	"internal/service/calendar_runner.go",
	"internal/service/calendar_handoff.go",
	"internal/doccheck/main.go",
	"internal/workflowcheck/baseline_test.go",
	"internal/workflowcheck/workflows_test.go",
	"internal/workflowcheck/doc.go",
}

// TestRiskPaths guards row B6 of frostyard/core
// docs/specs/repository-baseline.md: .github/risk-paths.json lists every
// governed path of the repository, with a reason, and no stale glob.
func TestRiskPaths(t *testing.T) {
	cfg := readRiskPaths(t)
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", cfg.SchemaVersion)
	}
	if len(cfg.Paths) == 0 {
		t.Fatal(".github/risk-paths.json lists no path")
	}
	globs := make([]string, 0, len(cfg.Paths))
	seen := make(map[string]bool, len(cfg.Paths))
	for i, p := range cfg.Paths {
		for _, problem := range validGlob(p.Glob) {
			t.Errorf("entry %d: glob %q: %s", i, p.Glob, problem)
		}
		if seen[p.Glob] {
			t.Errorf("entry %d: glob %q appears twice", i, p.Glob)
		}
		seen[p.Glob] = true
		globs = append(globs, p.Glob)
		if strings.TrimSpace(p.Why) == "" {
			t.Errorf("entry %d: glob %q has no why", i, p.Glob)
		}
		if !whyCategory(p.Why) {
			t.Errorf("entry %d: glob %q has the why %q; it must start with 1 of the categories %q and a colon", i, p.Glob, p.Why, riskCategories)
		}
		if n := utf8.RuneCountInString(p.Why); n > 200 {
			t.Errorf("entry %d: glob %q has a why of %d characters, want 200 at most", i, p.Glob, n)
		}
	}

	want := slices.Clone(governedPaths)
	workflows, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range workflows {
		want = append(want, path.Join(".github/workflows", filepath.Base(p)))
	}
	files := repoFiles(t)
	for _, p := range missingPaths(want, files) {
		t.Errorf("%s is missing; the repository must have it, and .github/risk-paths.json must list it", p)
	}
	for _, p := range want {
		if !matchAny(globs, p) {
			t.Errorf("%s is a release, secrets, auth or destructive path; .github/risk-paths.json must list it", p)
		}
	}

	for _, g := range globs {
		if !anyMatch(g, files) {
			t.Errorf("glob %q matches no file in the repository", g)
		}
	}
	t.Logf("%d globs; %d required paths listed; %d files searched", len(globs), len(want), len(files))
}

// missingPaths returns each of required that is not in files, in order. A
// required path must exist: a glob such as .github/workflows/** still matches
// the name of a deleted caller.
func missingPaths(required, files []string) []string {
	var missing []string
	for _, p := range required {
		if !slices.Contains(files, p) {
			missing = append(missing, p)
		}
	}
	return missing
}

// TestMissingPaths checks that a required path that is not in the repository is
// missing, also when a wide glob still matches its name.
func TestMissingPaths(t *testing.T) {
	files := []string{".github/risk-paths.json", ".github/workflows/ci.yml"}
	required := []string{".github/risk-paths.json", ".github/workflows/risk-paths.yml"}
	if !matchAny([]string{".github/workflows/**"}, ".github/workflows/risk-paths.yml") {
		t.Fatal("the wide glob does not match the caller's name")
	}
	got := missingPaths(required, files)
	if want := []string{".github/workflows/risk-paths.yml"}; !slices.Equal(got, want) {
		t.Errorf("missingPaths(%q, %q) = %q, want %q", required, files, got, want)
	}
	if got := missingPaths(files, files); len(got) != 0 {
		t.Errorf("missingPaths of present paths = %q, want none", got)
	}
}

// matchAny reports whether p matches 1 of globs.
func matchAny(globs []string, p string) bool {
	for _, g := range globs {
		if matchGlob(g, p) {
			return true
		}
	}
	return false
}

// anyMatch reports whether glob matches 1 of paths.
func anyMatch(glob string, paths []string) bool {
	for _, p := range paths {
		if matchGlob(glob, p) {
			return true
		}
	}
	return false
}

// maxBraceExpansions is the most globs 1 brace group set may give
// (frostyard/core scripts/lib/risk-paths.mjs).
const maxBraceExpansions = 256

// matchGlob reports whether glob matches the whole slash-separated path
// (frostyard/core docs/specs/risk-paths.md): {a,b} is 1 of the alternatives,
// ** as a whole segment is 0 or more whole segments, * and ? never cross a
// slash, and every other character is literal. An empty path matches nothing,
// and a glob that cannot be expanded matches nothing.
func matchGlob(glob, p string) bool {
	if p == "" {
		return false
	}
	expansions, errs := expandBraces(glob)
	if len(errs) > 0 {
		return false
	}
	pathSegments := strings.Split(p, "/")
	for _, expansion := range expansions {
		if matchSegments(strings.Split(expansion, "/"), pathSegments) {
			return true
		}
	}
	return false
}

// expandBraces returns 1 expansion of glob for each combination of its brace
// groups, and the reasons glob cannot be expanded; a glob with a reason expands
// to nothing. Groups do not nest.
func expandBraces(glob string) (expansions []string, errs []string) {
	var parts [][]string
	literal := ""
	for i := 0; i < len(glob); {
		switch glob[i] {
		case '}':
			errs = append(errs, "unmatched '}'")
			i++
		case '{':
			rest := glob[i+1:]
			close, nextOpen := strings.IndexByte(rest, '}'), strings.IndexByte(rest, '{')
			if close < 0 {
				return nil, append(errs, "unmatched '{'")
			}
			if nextOpen >= 0 && nextOpen < close {
				return nil, append(errs, "nested braces are not supported")
			}
			alternatives := strings.Split(rest[:close], ",")
			if len(alternatives) < 2 {
				errs = append(errs, "a brace group needs at least 2 alternatives")
			}
			if slices.Contains(alternatives, "") {
				errs = append(errs, "a brace group has an empty alternative")
			}
			parts = append(parts, []string{literal}, alternatives)
			literal = ""
			i += close + 2
		default:
			literal += glob[i : i+1]
			i++
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	parts = append(parts, []string{literal})

	expansions = []string{""}
	for _, alternatives := range parts {
		next := make([]string, 0, len(expansions)*len(alternatives))
		for _, prefix := range expansions {
			for _, alternative := range alternatives {
				next = append(next, prefix+alternative)
			}
		}
		if len(next) > maxBraceExpansions {
			return nil, []string{fmt.Sprintf("brace expansion gives more than %d globs", maxBraceExpansions)}
		}
		expansions = next
	}
	return expansions, nil
}

// matchSegments reports whether the path segments match the glob segments,
// where a ** segment is 0 or more whole segments.
func matchSegments(globSegments, pathSegments []string) bool {
	patterns := make([]*regexp.Regexp, len(globSegments))
	for i, segment := range globSegments {
		if segment != "**" {
			patterns[i] = segmentPattern(segment)
		}
	}
	memo := make(map[[2]int]bool, len(globSegments)*len(pathSegments)+1)
	var visit func(g, p int) bool
	visit = func(g, p int) bool {
		if match, ok := memo[[2]int{g, p}]; ok {
			return match
		}
		var match bool
		switch {
		case g == len(patterns):
			match = p == len(pathSegments)
		case patterns[g] == nil:
			match = visit(g+1, p) || (p < len(pathSegments) && visit(g, p+1))
		default:
			match = p < len(pathSegments) && patterns[g].MatchString(pathSegments[p]) && visit(g+1, p+1)
		}
		memo[[2]int{g, p}] = match
		return match
	}
	return visit(0, 0)
}

// segmentPattern compiles 1 glob segment: * is any run of characters within the
// segment, ? is 1 such character, and every other character is literal.
func segmentPattern(segment string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, c := range segment {
		switch c {
		case '*':
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// validGlob returns the reasons glob is not a path glob of frostyard/core
// docs/specs/risk-paths.md; no reason means a valid glob.
func validGlob(glob string) []string {
	if glob == "" {
		return []string{"glob is empty"}
	}
	var errs []string
	for _, c := range []string{"[", "]", "!", `\`} {
		if strings.Contains(glob, c) {
			errs = append(errs, fmt.Sprintf("%q is not supported", c))
		}
	}
	expansions, braceErrs := expandBraces(glob)
	errs = append(errs, braceErrs...)
	for _, expansion := range expansions {
		if strings.HasPrefix(expansion, "/") {
			errs = append(errs, "a glob must not start with '/'")
		}
		if strings.HasPrefix(expansion, "./") {
			errs = append(errs, "a glob must not start with './'")
		}
		for _, segment := range strings.Split(expansion, "/") {
			switch {
			case segment == "":
				errs = append(errs, "a glob must not have an empty segment")
			case segment == "..":
				errs = append(errs, "a glob must not have a '..' segment")
			case strings.Contains(segment, "**") && segment != "**":
				errs = append(errs, "'**' must be a whole segment")
			}
		}
	}
	return unique(errs)
}

// unique returns list without a repeated entry, in the order of 1st appearance.
func unique(list []string) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// TestValidGlob checks the glob validation of frostyard/core
// docs/specs/risk-paths.md on the cases a repository's list can hold.
func TestValidGlob(t *testing.T) {
	for _, glob := range []string{"a/**", "{a,b}/c", "**"} {
		if problems := validGlob(glob); len(problems) > 0 {
			t.Errorf("validGlob(%q) = %v, want no problem", glob, problems)
		}
	}
	for _, glob := range []string{
		"internal/auth/{auth.go,}",
		"{a}",
		"{a,{b,c}}",
		"a}",
		"{a,b",
		"[ab]",
		"!a",
		`a\b`,
		"a**",
		"a/**b",
		"/a",
		"./a",
		"a//b",
		"a/../b",
		strings.Repeat("{a,b}", 9),
	} {
		if problems := validGlob(glob); len(problems) == 0 {
			t.Errorf("validGlob(%q) = no problem, want at least 1", glob)
		}
	}
}

// TestWhyCategory checks that a why names 1 category of frostyard/core
// docs/specs/risk-paths.md, before a colon.
func TestWhyCategory(t *testing.T) {
	for why, want := range map[string]bool{
		"auth: the OIDC login":                  true,
		"destructive operations: a restore":     true,
		"security: the address guard":           false,
		"auth the OIDC login":                   false,
		"Auth: the OIDC login":                  false,
		"governance":                            false,
		" governance: the risk list":            false,
		"secrets and credentials: the examples": true,
	} {
		if got := whyCategory(why); got != want {
			t.Errorf("whyCategory(%q) = %v, want %v", why, got, want)
		}
	}
}

// TestMatchGlob checks the glob rules of frostyard/core
// docs/specs/risk-paths.md on its own cases and the ones this repository's
// list holds.
func TestMatchGlob(t *testing.T) {
	for _, tc := range []struct {
		glob string
		path string
		want bool
	}{
		{"**/x", "x", true},
		{"**/x", "a/x", true},
		{"**/x", "a/b/x", true},
		{"**/x", "a/x/y", false},
		{"**", "x", true},
		{"**", "a/b", true},
		{"**", ".github/x.yml", true},
		{"x*", "x", true},
		{"x*", "xy", true},
		{"x*", "a/x", false},
		{"x*", "a/xy", false},
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/.md", true},
		{"docs/*.md", "docs/a/b.md", false},
		{"docs/*.md", "src/a.md", false},
		{"internal/backup/**", "internal/backup/backup.go", true},
		{"internal/backup/**", "internal/backup/restore/restore.go", true},
		{"internal/backup/**", "internal/service/worker.go", false},
		{".github/{risk-paths.json,settings.yml}", ".github/risk-paths.json", true},
		{".github/{risk-paths.json,settings.yml}", ".github/settings.yml", true},
		{".github/{risk-paths.json,settings.yml}", ".github/workflows/ci.yml", false},
		{".github/workflows/**", ".github/workflows/ci.yml", true},
		{".github/workflows/**", ".github/risk-paths.json", false},
		{"internal/{workflowcheck,doccheck}/**", "internal/doccheck/main.go", true},
		{"internal/{workflowcheck,doccheck}/**", "internal/workflowcheck/baseline_test.go", true},
		{"internal/{workflowcheck,doccheck}/**", "internal/house/house.go", false},
		{"internal/trimlight/{client,control,playback,protocol}.go", "internal/trimlight/control.go", true},
		{"internal/trimlight/{client,control,playback,protocol}.go", "internal/trimlight/client_test.go", false},
		{"internal/trimlight/{client,control,playback,protocol}.go", "internal/trimlight/session.go", false},
		{"a/**/x", "a/x", true},
		{"a/**/x", "a/b/x", true},
		{"deploy/**", "deploy/oidc.example.json", true},
		{"deploy/**", "internal/auth/auth.go", false},
		{"{Containerfile,.dockerignore}", "Containerfile", true},
		{"{Containerfile,.dockerignore}", ".dockerignore", true},
		{"{Containerfile,.dockerignore}", "Dockerfile", false},
		{"{mise.toml,mise.lock}", "mise.toml", true},
		{"{mise.toml,mise.lock}", "mise.lock", true},
		{"{mise.toml,mise.lock}", "mise.tom", false},
		{"Makefile", "Makefile", true},
		{"Makefile", "app/Makefile", false},
		{"a/*", "a/b", true},
		{"a/*", "a/b/c", false},
		{"**", "", false},
		{"*.md", "", false},
	} {
		if got := matchGlob(tc.glob, tc.path); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
}

// dependabotEcosystem names the Dependabot package-ecosystem of the manifest
// file base, if base is a manifest.
func dependabotEcosystem(base string) (string, bool) {
	switch base {
	case "go.mod":
		return "gomod", true
	case "package.json":
		return "npm", true
	case "Cargo.toml":
		return "cargo", true
	case "Dockerfile", "Containerfile":
		return "docker", true
	}
	return "", false
}

// normalizeDir drops the trailing slash of a Dependabot directory, and keeps
// the root as "/".
func normalizeDir(dir string) string {
	if dir == "/" {
		return dir
	}
	return strings.TrimSuffix(dir, "/")
}

// TestDependabotCoversManifests guards row B13 of frostyard/core
// docs/specs/repository-baseline.md: .github/dependabot.yml has 1 entry for
// each manifest of the repository, and the github-actions entry at "/".
func TestDependabotCoversManifests(t *testing.T) {
	const name = ".github/dependabot.yml"
	fields := readYAML(t, name)
	version, ok := topLevel(fields, "version")
	if !ok {
		t.Fatalf("%s: no version", name)
	}
	if version.value != "2" {
		t.Errorf("%s:%d: version is %q, want 2", name, version.line, version.value)
	}

	covered := make(map[[2]string]bool)
	actions := false
	for _, f := range fields {
		if f.key != "package-ecosystem" {
			continue
		}
		dir, ok := itemField(fields, f.item, "directory")
		if !ok {
			t.Errorf("%s:%d: the %s entry has no directory", name, f.line, f.value)
			continue
		}
		entry := [2]string{f.value, normalizeDir(dir.value)}
		covered[entry] = true
		if entry == [2]string{"github-actions", "/"} {
			actions = true
		}
	}
	if !actions {
		t.Errorf("%s: no github-actions entry with directory /", name)
	}

	manifests := 0
	for _, f := range repoFiles(t) {
		eco, ok := dependabotEcosystem(path.Base(f))
		if !ok {
			continue
		}
		manifests++
		dir := path.Dir(f)
		want := "/"
		if dir != "." {
			want = "/" + dir
		}
		if !covered[[2]string{eco, want}] {
			t.Errorf("%s has no Dependabot entry (package-ecosystem %s, directory %s)", f, eco, want)
		}
	}
	if manifests == 0 {
		t.Fatal("no dependency manifest found")
	}
	t.Logf("%d manifests, %d Dependabot entries", manifests, len(covered))
}

// riskMarkers returns the source fragments that mark a Go file as a risk path:
// a delete of rows, of a photo or of a file, a write to the controller, a
// bounded controller write, a backup or a restore of a bundle that holds the
// OIDC secret, the client's paced session, a private file mode, and the OIDC
// secret or the private API token. Each names a guard that lives in 1 file
// today and could move to another.
func riskMarkers() []string {
	return []string{
		"DELETE FROM",
		".DeletePhoto(",
		"os.Remove",
		".Control(",
		".ProbeSchedule2(",
		".VerifyPlayback(",
		"backup.Restore(",
		"backup.Create(",
		"withSession(",
		"0o600",
		"ClientSecret",
		"readToken(",
	}
}

// riskMarker returns the first marker of riskMarkers() that src holds, in the
// order of the list; no marker means plain source.
func riskMarker(src string) (string, bool) {
	for _, marker := range riskMarkers() {
		if strings.Contains(src, marker) {
			return marker, true
		}
	}
	return "", false
}

// TestRiskMarkers guards the risk list against the next omission: a Go file
// that deletes rows, photos or files, writes to the controller, creates or
// restores a backup, holds the client's paced session, sets a private file mode
// or holds the OIDC secret or the private API token is a destructive, secrets or
// auth path, whatever package it is in. A glob that names files misses such a
// file when a function moves to a new one.
func TestRiskMarkers(t *testing.T) {
	cfg := readRiskPaths(t)
	globs := make([]string, 0, len(cfg.Paths))
	for _, p := range cfg.Paths {
		globs = append(globs, p.Glob)
	}
	marked := 0
	for _, f := range repoFiles(t) {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		if !strings.HasPrefix(f, "cmd/") && !strings.HasPrefix(f, "internal/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(f)))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		marker, ok := riskMarker(string(data))
		if !ok {
			continue
		}
		marked++
		if !matchAny(globs, f) {
			t.Errorf("%s has %s (a destructive, secrets or auth guard); .github/risk-paths.json must list it", f, marker)
		}
	}
	t.Logf("%d globs; %d Go files with a marker", len(globs), marked)
}

// TestRiskMarker checks the marker list on source that holds each marker, and
// on plain source.
func TestRiskMarker(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"delete of rows", "_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE hash=?`, hash)", "DELETE FROM"},
		{"delete of a photo", "if err := store.DeletePhoto(r.Context(), r.PathValue(\"id\")); err != nil {", ".DeletePhoto("},
		{"delete of a file", "return errors.Join(err, os.Remove(backup))", "os.Remove"},
		{"delete of a tree", "defer os.RemoveAll(temp)", "os.Remove"},
		{"controller write", "controlResult, err = w.controller.Control(ctx, *op, nil)", ".Control("},
		{"bounded probe", "report, probeErr := client.ProbeSchedule2(ctx, baseline, nil)", ".ProbeSchedule2("},
		{"bounded verification", "report, verifyErr := client.VerifyPlayback(ctx, baseline, target, nil)", ".VerifyPlayback("},
		{"restore of a bundle", "report, err = backup.Restore(cmd.Context(), input, output)", "backup.Restore("},
		{"backup of a bundle", "report, err = backup.Create(cmd.Context(), dir, output, config, version)", "backup.Create("},
		{"paced session", "err := c.withSession(ctx, func(s *session) error { return nil })", "withSession("},
		{"private file mode", "const privateMode fs.FileMode = 0o600", "0o600"},
		{"OIDC secret", "ClientSecret string `json:\"-\"`", "ClientSecret"},
		{"private API token file", "token, err := readToken(tokenFile)", "readToken("},
		{"first marker in the list order", "defer os.Remove(path)", "os.Remove"},
		{"plain source", "package house\n\nfunc Serve() {}\n", ""},
	} {
		got, ok := riskMarker(tc.src)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: riskMarker(%q) = %q, %v, want %q", tc.name, tc.src, got, ok, tc.want)
		}
	}
}
