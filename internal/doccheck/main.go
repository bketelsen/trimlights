// doccheck validates local Markdown links and canonical agent entry points.
package main

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var link = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

func anchors(text string) map[string]bool {
	out := map[string]bool{}
	counts := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "#")))
		var b strings.Builder
		for _, r := range title {
			if r == ' ' {
				b.WriteByte('-')
			} else if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_' {
				b.WriteRune(r)
			}
		}
		base := b.String()
		slug := base
		if n := counts[base]; n > 0 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		counts[base]++
		out[slug] = true
	}
	return out
}

func check() error {
	paths := []string{"README.md", "AGENTS.md"}
	if err := filepath.WalkDir("docs", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") && d.Name() != "TEMPLATE.md" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return err
	}
	count := 0
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range link.FindAllStringSubmatch(string(content), -1) {
			ref := strings.Trim(m[1], "<>")
			u, err := url.Parse(ref)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if u.IsAbs() {
				continue
			}
			target := path
			if u.Path != "" {
				target = filepath.Join(filepath.Dir(path), u.Path)
			}
			info, err := os.Stat(target)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", path, ref, err)
			}
			if u.Fragment != "" && !info.IsDir() {
				text, err := os.ReadFile(target)
				if err != nil {
					return err
				}
				if !anchors(string(text))[u.Fragment] {
					return fmt.Errorf("%s: missing anchor %s", path, ref)
				}
			}
			count++
		}
	}
	for path, target := range map[string]string{"CLAUDE.md": "AGENTS.md", "GEMINI.md": "AGENTS.md", ".github/copilot-instructions.md": "../AGENTS.md", ".claude/skills": "../.agents/skills"} {
		got, err := os.Readlink(path)
		if err != nil || got != target {
			return fmt.Errorf("%s must be a symlink to %s", path, target)
		}
		if _, err = os.Stat(path); err != nil {
			return err
		}
	}
	fmt.Printf("Checked %d local documentation links and agent entry points.\n", count)
	return nil
}
func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
