package workflowcheck

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

// The corrections inbox of row B2 (frostyard/core ADR-0081): 1 file for each
// record in correctionsDir. correctionsOld is the old single file, which this
// repository no longer has.
const (
	correctionsDir = ".memory/corrections"
	correctionsOld = ".memory/corrections.jsonl"
)

// recordName is the name of 1 record file: the record's date, then a slug of
// lowercase words. It is the pattern of frostyard/core
// scripts/check-corrections.mjs.
var recordName = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-([a-z0-9]+(?:-[a-z0-9]+)*)\.json$`)

// slugMax is the longest slug a record file name may carry.
const slugMax = 60

// canonicalRecord returns the canonical text of a record: the text that
// JavaScript's JSON.stringify(record, null, 2) gives with the 5 members in
// their fixed order, and 1 final newline (frostyard/core ADR-0081).
func canonicalRecord(record map[string]string) string {
	literals := make(map[string]string, len(record))
	for name, value := range record {
		literals[name] = quoteJS(value)
	}
	return canonicalLayout(literals)
}

// canonicalLayout returns the canonical text of a record whose members are
// given as their canonical JSON string literals, quotes included.
func canonicalLayout(literals map[string]string) string {
	var b strings.Builder
	b.WriteString("{\n")
	for i, name := range correctionsMember {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(`  "` + name + `": `)
		b.WriteString(literals[name])
	}
	b.WriteString("\n}\n")
	return b.String()
}

// quoteJS quotes s as JavaScript's JSON.stringify does. s must be valid
// UTF-8.
func quoteJS(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		writeJSRune(&b, r)
	}
	b.WriteByte('"')
	return b.String()
}

// writeJSRune writes 1 character of a string the way JSON.stringify does: it
// escapes the quote, the backslash and the control characters below U+0020
// (with a short escape where JSON has one, else \u00xx in lowercase), and
// writes every other character as it is.
func writeJSRune(b *strings.Builder, r rune) {
	switch r {
	case '"':
		b.WriteString(`\"`)
	case '\\':
		b.WriteString(`\\`)
	case '\b':
		b.WriteString(`\b`)
	case '\f':
		b.WriteString(`\f`)
	case '\n':
		b.WriteString(`\n`)
	case '\r':
		b.WriteString(`\r`)
	case '\t':
		b.WriteString(`\t`)
	default:
		if r < 0x20 {
			fmt.Fprintf(b, `\u%04x`, r)
		} else {
			b.WriteRune(r)
		}
	}
}

// isSurrogate, isHighSurrogate and isLowSurrogate classify 1 UTF-16 code
// unit.
func isSurrogate(u rune) bool     { return u >= 0xD800 && u <= 0xDFFF }
func isHighSurrogate(u rune) bool { return u >= 0xD800 && u <= 0xDBFF }
func isLowSurrogate(u rune) bool  { return u >= 0xDC00 && u <= 0xDFFF }

// unicodeEscape returns the code unit of the escape \uXXXX at the start of s,
// and whether s starts with one.
func unicodeEscape(s string) (rune, bool) {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
		return 0, false
	}
	u, err := strconv.ParseUint(s[2:6], 16, 16)
	if err != nil {
		return 0, false
	}
	return rune(u), true
}

// shortEscape returns the character of the JSON escape \c, and whether c
// makes one.
func shortEscape(c byte) (rune, bool) {
	switch c {
	case '"', '\\', '/':
		return rune(c), true
	case 'b':
		return '\b', true
	case 'f':
		return '\f', true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	}
	return 0, false
}

// stringLiteral holds 1 JSON string literal of a record text: its raw text,
// quotes included, as the file has it; the same text with each escaped lone
// surrogate replaced by \ufffd, which a Go JSON decoder accepts; and the
// literal JSON.stringify writes for its value.
type stringLiteral struct {
	raw, decodable, canonical string
}

// readLiteral reads the JSON string literal at the start of s, which starts
// with a quote. JSON.parse keeps an escaped lone UTF-16 surrogate, such as
// \ud800, in its string, and JSON.stringify writes it back as a lowercase
// escape; a Go string cannot hold it. So the literal is read here, 1 escape
// at a time. ok is false for a literal with no closing quote.
func readLiteral(s string) (lit stringLiteral, ok bool) {
	var decodable, canonical strings.Builder
	decodable.WriteByte('"')
	canonical.WriteByte('"')
	for i := 1; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			decodable.WriteByte('"')
			canonical.WriteByte('"')
			return stringLiteral{raw: s[:i+1], decodable: decodable.String(), canonical: canonical.String()}, true
		case c != '\\':
			r, size := utf8.DecodeRuneInString(s[i:])
			decodable.WriteString(s[i : i+size])
			writeJSRune(&canonical, r)
			i += size
		case i+1 >= len(s):
			return stringLiteral{}, false
		case s[i+1] != 'u':
			decodable.WriteString(s[i : i+2])
			if r, ok := shortEscape(s[i+1]); ok {
				writeJSRune(&canonical, r)
			}
			i += 2
		default:
			u, ok := unicodeEscape(s[i:])
			if !ok {
				// Not valid JSON: the decoder reports it.
				decodable.WriteString(s[i : i+2])
				i += 2
				continue
			}
			if low, ok := unicodeEscape(s[i+6:]); ok && isHighSurrogate(u) && isLowSurrogate(low) {
				decodable.WriteString(s[i : i+12])
				canonical.WriteRune(0x10000 + (u-0xD800)<<10 + (low - 0xDC00))
				i += 12
				continue
			}
			if isSurrogate(u) {
				decodable.WriteString(`\ufffd`)
				fmt.Fprintf(&canonical, `\u%04x`, u)
			} else {
				decodable.WriteString(s[i : i+6])
				writeJSRune(&canonical, u)
			}
			i += 6
		}
	}
	return stringLiteral{}, false
}

// recordLiterals returns the string literals of a record text in their
// order, and the text with each escaped lone surrogate replaced by \ufffd.
// Text outside the literals is copied as it is. A literal with no closing
// quote ends the scan; the decoder then reports invalid JSON.
func recordLiterals(text string) ([]stringLiteral, string) {
	var literals []stringLiteral
	var decodable strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '"' {
			decodable.WriteByte(text[i])
			i++
			continue
		}
		lit, ok := readLiteral(text[i:])
		if !ok {
			decodable.WriteString(text[i:])
			break
		}
		literals = append(literals, lit)
		decodable.WriteString(lit.decodable)
		i += len(lit.raw)
	}
	return literals, decodable.String()
}

// recordFileProblems returns the problems of the record file name with the
// content data, after frostyard/core scripts/check-corrections.mjs (row B2):
// the name, the content rules of correctionProblems, the date in the name, and
// the canonical form. No problem means a valid record.
func recordFileProblems(name string, data []byte) []string {
	match := recordName.FindStringSubmatch(name)
	if match == nil {
		return []string{"bad file name (want <date>-<slug>.json)"}
	}
	var problems []string
	if len(match[2]) > slugMax {
		problems = append(problems, fmt.Sprintf("slug longer than %d characters", slugMax))
	}
	if !utf8.Valid(data) {
		return append(problems, "invalid UTF-8")
	}
	literals, decodable := recordLiterals(string(data))
	content := correctionProblems(decodable)
	if len(content) > 0 {
		return append(problems, content...)
	}
	// The content is 1 object of the 5 string members, so its literals are a
	// name, then its value, 5 times, in the order of the text.
	canonical := make(map[string]string, len(correctionsMember))
	var date string
	for i := 0; i+1 < len(literals); i += 2 {
		var name, value string
		if err := json.Unmarshal([]byte(literals[i].decodable), &name); err != nil {
			return append(problems, fmt.Sprintf("invalid JSON: %v", err))
		}
		if err := json.Unmarshal([]byte(literals[i+1].decodable), &value); err != nil {
			return append(problems, fmt.Sprintf("invalid JSON: %v", err))
		}
		canonical[name] = literals[i+1].canonical
		if name == "date" {
			date = value
		}
	}
	if date != match[1] {
		problems = append(problems, fmt.Sprintf("name date %s does not equal date %s", match[1], date))
	}
	if string(data) != canonicalLayout(canonical) {
		problems = append(problems, "not in canonical form")
	}
	return problems
}

// correctionsProblems returns the problems of the corrections inbox in the
// repository tree fsys, by row B2 and frostyard/core ADR-0081, and the number
// of record files. The folder exists (an empty .gitkeep is enough), holds only
// record files and that .gitkeep, and the old single file is gone: this
// repository moved its records to the folder, and the old file does not come
// back. Symbolic links are not followed.
func correctionsProblems(fsys fs.FS) (int, []string) {
	var problems []string
	add := func(p, problem string) { problems = append(problems, p+": "+problem) }

	top, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return 0, []string{fmt.Sprintf("the repository root: %v", err)}
	}
	i := slices.IndexFunc(top, func(e fs.DirEntry) bool { return e.Name() == ".memory" })
	switch {
	case i < 0:
		return 0, []string{correctionsDir + ": missing"}
	case top[i].Type()&fs.ModeSymlink != 0:
		return 0, []string{".memory: symbolic link, not followed"}
	case !top[i].IsDir():
		return 0, []string{".memory: not a folder"}
	}

	memory, err := fs.ReadDir(fsys, ".memory")
	if err != nil {
		return 0, []string{fmt.Sprintf(".memory: %v", err)}
	}
	var folder fs.DirEntry
	for _, e := range memory {
		switch e.Name() {
		case path.Base(correctionsOld):
			add(correctionsOld, "is retired in this repository; add a file under "+correctionsDir+"/")
		case path.Base(correctionsDir):
			folder = e
		}
	}
	switch {
	case folder == nil:
		add(correctionsDir, "missing (an empty .gitkeep is enough when there is no record)")
		return 0, problems
	case folder.Type()&fs.ModeSymlink != 0:
		add(correctionsDir, "symbolic link, not followed")
		return 0, problems
	case !folder.IsDir():
		add(correctionsDir, "not a folder")
		return 0, problems
	}

	entries, err := fs.ReadDir(fsys, correctionsDir)
	if err != nil {
		add(correctionsDir, err.Error())
		return 0, problems
	}
	if len(entries) == 0 {
		add(correctionsDir, "empty (an empty .gitkeep is enough when there is no record)")
	}
	records := 0
	for _, e := range entries {
		p := correctionsDir + "/" + e.Name()
		if e.Type()&fs.ModeSymlink != 0 {
			add(p, "symbolic link, not followed")
			continue
		}
		if !e.Type().IsRegular() {
			add(p, "not a regular file")
			continue
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			add(p, err.Error())
			continue
		}
		if e.Name() == ".gitkeep" {
			if len(data) != 0 {
				add(p, ".gitkeep is not empty")
			}
			continue
		}
		if recordName.MatchString(e.Name()) {
			records++
		}
		for _, problem := range recordFileProblems(e.Name(), data) {
			add(p, problem)
		}
	}
	return records, problems
}

// TestCorrectionsInbox guards row B2 of frostyard/core
// docs/specs/repository-baseline.md in this repository: every file of
// .memory/corrections/ is 1 valid record in canonical form, and
// .memory/corrections.jsonl is gone. That no record is removed, renamed or
// changed is a rule this test does not check: it reads only this tree.
func TestCorrectionsInbox(t *testing.T) {
	records, problems := correctionsProblems(os.DirFS(filepath.Join("..", "..")))
	for _, problem := range problems {
		t.Error(problem)
	}
	t.Logf("%d correction records", records)
}

// validRecord is the canonical text of a valid record file named
// 2026-11-13-pr-412-cache-key.json.
const validRecord = `{
  "date": "2026-11-13",
  "scope": "ci",
  "correction": "Pin the cache key to mise.lock.",
  "evidence": "frostyard/pocket#412, cross-review of head 1a2b3c4",
  "promoted_to": "test/cache-key.test.mjs"
}
`

// TestCanonicalRecord checks the canonical text against JSON.stringify's
// output for the same records, which node gives for the text in each case.
func TestCanonicalRecord(t *testing.T) {
	record := func(correction string) map[string]string {
		return map[string]string{"date": "2026-11-13", "scope": "s", "correction": correction, "evidence": "e", "promoted_to": ""}
	}
	head := "{\n  \"date\": \"2026-11-13\",\n  \"scope\": \"s\",\n  \"correction\": "
	tail := ",\n  \"evidence\": \"e\",\n  \"promoted_to\": \"\"\n}\n"
	for _, tc := range []struct {
		name, correction, want string
	}{
		{"plain", "a b", `"a b"`},
		{"quote and backslash", `say "x" \ y`, `"say \"x\" \\ y"`},
		{"short escapes", "\b\f\n\r\t", `"\b\f\n\r\t"`},
		{"other control characters in lowercase hex", "\x00\x1f\x0b", `"\u0000\u001f\u000b"`},
		{"HTML characters as they are", "<a & b>", `"<a & b>"`},
		{"slash as it is", "a/b", `"a/b"`},
		{"non-ASCII as it is", "é — ✓ 😀", `"é — ✓ 😀"`},
		{"line and paragraph separators as they are", "\u2028\u2029", "\"\u2028\u2029\""},
		{"delete as it is", "\x7f", "\"\x7f\""},
	} {
		if got, want := canonicalRecord(record(tc.correction)), head+tc.want+tail; got != want {
			t.Errorf("%s: canonicalRecord = %q, want %q", tc.name, got, want)
		}
	}
	var example map[string]string
	if err := json.Unmarshal([]byte(validRecord), &example); err != nil {
		t.Fatal(err)
	}
	if got := canonicalRecord(example); got != validRecord {
		t.Errorf("canonicalRecord of the example = %q, want %q", got, validRecord)
	}
}

// TestRecordFileProblems checks the name, content, date and form rules of 1
// record file, on the cases frostyard/core scripts/check-corrections.mjs names.
func TestRecordFileProblems(t *testing.T) {
	const name = "2026-11-13-pr-412-cache-key.json"
	unpromoted := strings.Replace(validRecord, `"test/cache-key.test.mjs"`, `""`, 1)
	for _, tc := range []struct {
		label, name, text, want string // want: a part of the expected problem, empty when valid
	}{
		{"valid", name, validRecord, ""},
		{"unpromoted", name, unpromoted, ""},
		{"legacy record", "2026-11-13-legacy-0001.json", validRecord, ""},
		{"no slug", "2026-11-13.json", validRecord, "bad file name"},
		{"uppercase slug", "2026-11-13-PR-412.json", validRecord, "bad file name"},
		{"double hyphen", "2026-11-13-pr--412.json", validRecord, "bad file name"},
		{"trailing hyphen", "2026-11-13-pr-412-.json", validRecord, "bad file name"},
		{"old extension", "2026-11-13-pr-412.jsonl", validRecord, "bad file name"},
		{"no date", "pr-412-cache-key.json", validRecord, "bad file name"},
		{"slug of 61 characters", "2026-11-13-" + strings.Repeat("a", 61) + ".json", validRecord, "slug longer than 60"},
		{"slug of 60 characters", "2026-11-13-" + strings.Repeat("a", 60) + ".json", validRecord, ""},
		{"name date differs", "2026-11-12-pr-412-cache-key.json", validRecord, "name date 2026-11-12 does not equal date 2026-11-13"},
		{"compact JSON", name, `{"date":"2026-11-13","scope":"ci","correction":"c","evidence":"e","promoted_to":""}` + "\n", "not in canonical form"},
		{"keys out of order", name, strings.Replace(validRecord, "  \"date\": \"2026-11-13\",\n  \"scope\": \"ci\",\n", "  \"scope\": \"ci\",\n  \"date\": \"2026-11-13\",\n", 1), "not in canonical form"},
		{"no final newline", name, strings.TrimSuffix(validRecord, "\n"), "not in canonical form"},
		{"2 final newlines", name, validRecord + "\n", "not in canonical form"},
		{"CRLF", name, strings.ReplaceAll(validRecord, "\n", "\r\n"), "not in canonical form"},
		{"tab indent", name, strings.ReplaceAll(validRecord, "  \"", "\t\""), "not in canonical form"},
		{"escaped non-ASCII", name, strings.Replace(validRecord, "Pin the", `Pin th\u00e9`, 1), "not in canonical form"},
		{"escaped slash", name, strings.Replace(validRecord, "test/cache", `test\/cache`, 1), "not in canonical form"},
		{"uppercase hex escape", name, strings.Replace(validRecord, "Pin the", `Pin\u001Fthe`, 1), "not in canonical form"},
		{"lone high surrogate", name, strings.Replace(validRecord, "Pin the", `Pin\ud800the`, 1), ""},
		{"lone low surrogate", name, strings.Replace(validRecord, "Pin the", `Pin\udfffthe`, 1), ""},
		{"high surrogate before a letter", name, strings.Replace(validRecord, "Pin the", `Pin\ud800Athe`, 1), ""},
		{"2 high surrogates", name, strings.Replace(validRecord, "Pin the", `Pin\ud800\ud800the`, 1), ""},
		{"lone surrogate in uppercase", name, strings.Replace(validRecord, "Pin the", `Pin\uD800the`, 1), "not in canonical form"},
		{"escaped surrogate pair", name, strings.Replace(validRecord, "Pin the", `Pin\ud83d\ude00the`, 1), "not in canonical form"},
		{"raw astral character", name, strings.Replace(validRecord, "Pin the", "Pin\U0001F600the", 1), ""},
		{"escaped letter", name, strings.Replace(validRecord, "Pin the", `Pin\u0074he`, 1), "not in canonical form"},
		{"bad escape", name, strings.Replace(validRecord, "Pin the", `Pin\xthe`, 1), "invalid JSON"},
		{"unclosed string", name, `{"date": "2026-11-13`, "invalid JSON"},
		{"invalid UTF-8", name, strings.Replace(validRecord, "Pin", "P\xffn", 1), "invalid UTF-8"},
		{"missing member", name, strings.Replace(validRecord, ",\n  \"promoted_to\": \"test/cache-key.test.mjs\"", "", 1), "missing key(s): promoted_to"},
		{"unknown member", name, strings.Replace(validRecord, "\n}", ",\n  \"extra\": \"x\"\n}", 1), "unknown key(s): extra"},
		{"duplicate member", name, strings.Replace(validRecord, "  \"scope\"", "  \"date\": \"2026-11-13\",\n  \"scope\"", 1), "appears twice"},
		{"empty scope", name, strings.Replace(validRecord, `"scope": "ci"`, `"scope": " "`, 1), "scope is empty"},
		{"invalid date", "2026-02-30-pr-412-cache-key.json", strings.Replace(validRecord, "2026-11-13", "2026-02-30", 1), "invalid date: 2026-02-30"},
		{"not an object", name, "[]\n", "not a plain object"},
		{"empty file", name, "", "blank line"},
	} {
		problems := recordFileProblems(tc.name, []byte(tc.text))
		switch {
		case tc.want == "" && len(problems) > 0:
			t.Errorf("%s: recordFileProblems = %q, want none", tc.label, problems)
		case tc.want != "" && !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }):
			t.Errorf("%s: recordFileProblems = %q, want a problem with %q", tc.label, problems, tc.want)
		}
	}
}

// TestCorrectionsProblems checks the folder rules of row B2 on small trees:
// the folder exists, holds only record files and an empty .gitkeep, and the
// old single file does not come back.
func TestCorrectionsProblems(t *testing.T) {
	const record = correctionsDir + "/2026-11-13-pr-412-cache-key.json"
	file := func(text string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(text)} }
	for _, tc := range []struct {
		name    string
		fsys    fstest.MapFS
		records int
		want    string // a part of the expected problem, empty when valid
	}{
		{"1 record", fstest.MapFS{record: file(validRecord)}, 1, ""},
		{"only .gitkeep", fstest.MapFS{correctionsDir + "/.gitkeep": file("")}, 0, ""},
		{"record and .gitkeep", fstest.MapFS{record: file(validRecord), correctionsDir + "/.gitkeep": file("")}, 1, ""},
		{"README beside the folder", fstest.MapFS{record: file(validRecord), ".memory/README.md": file("# inbox\n")}, 1, ""},
		{".gitkeep not empty", fstest.MapFS{correctionsDir + "/.gitkeep": file("x")}, 0, ".gitkeep is not empty"},
		{"old file only", fstest.MapFS{correctionsOld: file("")}, 0, correctionsOld + ": is retired"},
		{"old file and folder", fstest.MapFS{record: file(validRecord), correctionsOld: file("")}, 1, correctionsOld + ": is retired"},
		{"no .memory", fstest.MapFS{"README.md": file("x")}, 0, correctionsDir + ": missing"},
		{"no folder", fstest.MapFS{".memory/README.md": file("x")}, 0, correctionsDir + ": missing"},
		{"empty folder", fstest.MapFS{correctionsDir: &fstest.MapFile{Mode: fs.ModeDir}}, 0, correctionsDir + ": empty"},
		{"folder is a file", fstest.MapFS{correctionsDir: file("x")}, 0, correctionsDir + ": not a folder"},
		{"folder is a link", fstest.MapFS{correctionsDir: &fstest.MapFile{Data: []byte("elsewhere"), Mode: fs.ModeSymlink}}, 0, correctionsDir + ": symbolic link"},
		{".memory is a link", fstest.MapFS{".memory": &fstest.MapFile{Data: []byte("elsewhere"), Mode: fs.ModeSymlink}}, 0, ".memory: symbolic link"},
		{"record is a link", fstest.MapFS{record: &fstest.MapFile{Data: []byte("elsewhere"), Mode: fs.ModeSymlink}}, 0, record + ": symbolic link"},
		{"subfolder", fstest.MapFS{correctionsDir + "/old/2026-11-13-pr-1-x.json": file(validRecord)}, 0, correctionsDir + "/old: not a regular file"},
		{"other file", fstest.MapFS{correctionsDir + "/notes.md": file("x")}, 0, correctionsDir + "/notes.md: bad file name"},
		{"bad record", fstest.MapFS{record: file("{}\n")}, 1, record + ": missing key(s)"},
	} {
		records, problems := correctionsProblems(tc.fsys)
		switch {
		case tc.want == "" && len(problems) > 0:
			t.Errorf("%s: correctionsProblems = %q, want none", tc.name, problems)
		case tc.want != "" && !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }):
			t.Errorf("%s: correctionsProblems = %q, want a problem with %q", tc.name, problems, tc.want)
		}
		if records != tc.records {
			t.Errorf("%s: correctionsProblems counts %d records, want %d", tc.name, records, tc.records)
		}
	}
}
