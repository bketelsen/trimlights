package workflowcheck

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	commitAction = regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	dockerAction = regexp.MustCompile(`^docker://[^@]+@sha256:[0-9a-f]{64}$`)
)

// immutable reports whether an action reference names one version for good: a
// local action, a commit, or an image digest. A reusable workflow of another
// repository is a commit too: frostyard/core/.github/workflows/pr-triage.yml
// has no release tags, so the caller pins its main commit.
func immutable(ref string) bool {
	return strings.HasPrefix(ref, "./") || commitAction.MatchString(ref) || dockerAction.MatchString(ref)
}

func TestPinnedReferencesAreImmutable(t *testing.T) {
	sha, digest := strings.Repeat("a", 40), strings.Repeat("b", 64)
	for ref, want := range map[string]bool{
		"actions/checkout@" + sha:                               true,
		"owner/repo/.github/actions/publish@" + sha:             true,
		"frostyard/core/.github/workflows/pr-triage.yml@" + sha: true,
		"docker://alpine@sha256:" + digest:                      true,
		"./.github/actions/check":                               true,
		"actions/checkout@v7":                                   false,
		"owner/repo/.github/actions/publish@main":               false,
		"frostyard/core/.github/workflows/pr-triage.yml@main":   false,
		"actions/checkout@deadbeef":                             false,
		"actions/checkout@${{inputs.ref}}":                      false,
		"docker://alpine:3.22":                                  false,
		"actions/checkout@" + strings.ToUpper(sha):              false,
	} {
		if got := immutable(ref); got != want {
			t.Errorf("immutable(%q) = %v, want %v", ref, got, want)
		}
	}
}

// yamlField is 1 "key: value" line of a workflow or of .github/dependabot.yml:
// the key, its value without a trailing comment, and the sequence item that
// holds the field.
type yamlField struct {
	line   int // the 1-based line number of the key
	item   int // the line number of the "-" that opens the innermost item that holds the field; 0 outside a sequence
	indent int // the column of the key
	key    string
	value  string
}

// scanYAML returns the fields of a YAML document that the checks read.
//
// The checks need a key, a scalar value and the sequence item that holds a
// field, and nothing else, so this reads those 3 and no more. A line of a block
// scalar (run: |, labels: |) is script text, not a field, and is skipped; a
// comment is dropped.
func scanYAML(data []byte) []yamlField {
	var fields []yamlField
	type open struct{ dash, line int }
	var stack []open
	block := -1 // the column of a block scalar's key, -1 outside one
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if block >= 0 {
			if strings.TrimSpace(line) == "" || indentOf(line) > block {
				continue
			}
			block = -1
		}
		text := stripComment(line)
		if strings.TrimSpace(text) == "" {
			continue
		}
		column := indentOf(text)
		content := text[column:]
		item := 0
		if rest, ok := cutItem(content); ok {
			for len(stack) > 0 && stack[len(stack)-1].dash >= column {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, open{dash: column, line: i + 1})
			item = i + 1
			column += len(content) - len(rest)
			content = rest
			if strings.TrimSpace(content) == "" {
				continue
			}
		} else {
			for len(stack) > 0 && stack[len(stack)-1].dash >= column {
				stack = stack[:len(stack)-1]
			}
			if len(stack) > 0 {
				item = stack[len(stack)-1].line
			}
		}
		key, value, ok := splitKeyValue(content)
		if !ok {
			continue
		}
		fields = append(fields, yamlField{line: i + 1, item: item, indent: column, key: key, value: value})
		if isBlockScalar(value) {
			block = column
		}
	}
	return fields
}

// indentOf returns the number of leading spaces of line.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// cutItem returns the content of a sequence item after its "- ", and whether
// the line starts an item.
func cutItem(content string) (string, bool) {
	if content == "-" {
		return "", true
	}
	if !strings.HasPrefix(content, "- ") {
		return content, false
	}
	return strings.TrimLeft(content[1:], " "), true
}

// stripComment returns line without a trailing comment: a "#" at the start of
// the content or after a space, outside quotes. A "#" that a value or an
// expression holds stays.
func stripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return strings.TrimRight(line, " \t")
}

// splitKeyValue returns the key and the value of a mapping entry, and whether
// content is one: the first ":" outside quotes that is followed by a space or
// ends the content.
func splitKeyValue(content string) (key, value string, ok bool) {
	var quote byte
	for i := 0; i < len(content); i++ {
		c := content[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ':':
			if i+1 < len(content) && content[i+1] != ' ' && content[i+1] != '\t' {
				continue
			}
			key = strings.TrimSpace(content[:i])
			return key, strings.TrimSpace(content[i+1:]), key != ""
		}
	}
	return "", "", false
}

// isBlockScalar reports whether value starts a block scalar: | or >, with an
// optional chomping indicator.
func isBlockScalar(value string) bool {
	switch value {
	case "|", "|-", "|+", ">", ">-", ">+":
		return true
	}
	return false
}

// readYAML returns the fields of the repository file name.
func readYAML(t *testing.T, name string) []yamlField {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return scanYAML(data)
}

// topLevel returns the field of key at the top level of the document.
func topLevel(fields []yamlField, key string) (yamlField, bool) {
	for _, f := range fields {
		if f.item == 0 && f.indent == 0 && f.key == key {
			return f, true
		}
	}
	return yamlField{}, false
}

// itemField returns the field of key that the sequence item holds.
func itemField(fields []yamlField, item int, key string) (yamlField, bool) {
	for _, f := range fields {
		if f.item == item && f.key == key {
			return f, true
		}
	}
	return yamlField{}, false
}

// workflows returns the fields of every workflow in .github/workflows, by the
// workflow's file name.
func workflows(t *testing.T) map[string][]yamlField {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.y*ml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no workflow found")
	}
	out := make(map[string][]yamlField, len(paths))
	for _, p := range paths {
		name := path.Join(".github/workflows", filepath.Base(p))
		out[filepath.Base(p)] = readYAML(t, name)
	}
	return out
}

// TestScanYAML checks the scanner on a document that holds the shapes the
// checks depend on: a block scalar whose text looks like a field, a comment, a
// step item with its with: mapping, and a top-level permissions.
func TestScanYAML(t *testing.T) {
	const doc = `# a comment
name: Check
on:
  push:
    branches: ['**']

permissions: {}

jobs:
  check:
    permissions:
      contents: read
    steps:
      - name: Run
        run: |
          # a comment of the script
          uses: actions/checkout@v7
          echo "tag: $TAG" >&2
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
  caller:
    uses: frostyard/core/.github/workflows/pr-triage.yml@c996ea3fb54bcf1a3c621113522213702a7673e2 # main
`
	fields := scanYAML([]byte(doc))

	perm, ok := topLevel(fields, "permissions")
	if !ok {
		t.Fatal("no top-level permissions")
	}
	if perm.value != "{}" {
		t.Errorf("top-level permissions = %q, want {}", perm.value)
	}

	var uses []yamlField
	for _, f := range fields {
		if f.key == "uses" {
			uses = append(uses, f)
		}
	}
	if len(uses) != 2 {
		t.Fatalf("found %d uses fields, want 2 (the script line of the block scalar is not 1)", len(uses))
	}
	if want := "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"; uses[0].value != want {
		t.Errorf("the first uses value = %q, want %q", uses[0].value, want)
	}
	if got := uses[1].value; !strings.HasPrefix(got, "frostyard/core/.github/workflows/pr-triage.yml@") {
		t.Errorf("the caller's uses value = %q, want the reusable workflow", got)
	}
	if item, ok := itemField(fields, uses[0].item, "persist-credentials"); !ok || item.value != "false" {
		t.Errorf("the checkout item holds persist-credentials %q, %v, want false", item.value, ok)
	}
	if _, ok := topLevel(fields, "uses"); ok {
		t.Error("the caller's uses is at the top level, want it inside its job")
	}
}

// Every external action is pinned to a full commit SHA (the version a trailing
// comment names is for people; the SHA is what runs), every workflow starts
// from declared permissions, and every checkout drops the token it would
// otherwise leave in .git/config.
func TestWorkflowsFollowADR0021(t *testing.T) {
	byName := workflows(t)
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	slices.Sort(names)

	uses := 0
	for _, name := range names {
		fields := byName[name]
		if perm, ok := topLevel(fields, "permissions"); !ok {
			t.Errorf("%s: no top-level permissions (start from permissions: {} and grant each job what it needs)", name)
		} else if perm.value != "{}" {
			t.Errorf("%s:%d: top-level permissions is %q, want {}", name, perm.line, perm.value)
		}
		for _, f := range fields {
			if f.key != "uses" {
				continue
			}
			uses++
			if !immutable(f.value) {
				t.Errorf("%s:%d: %q must name a full commit SHA or image digest", name, f.line, f.value)
			}
			if !strings.HasPrefix(f.value, "actions/checkout@") {
				continue
			}
			if pc, ok := itemField(fields, f.item, "persist-credentials"); !ok || pc.value != "false" {
				t.Errorf("%s:%d: checkout must set persist-credentials: false", name, f.line)
			}
		}
	}
	if uses == 0 {
		t.Fatal("no action references found")
	}
}
