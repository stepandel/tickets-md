// Package archtest enforces the dependency-direction rules documented
// in AGENTS.md. It does not ship in the built binary; it only runs as
// part of `go test ./...` (and therefore `make check`).
//
// The rules codified here:
//
//  1. `internal/cli` is a sink — no other `internal/*` package may
//     import it.
//  2. `internal/terminal` is private to `cli` — only `internal/cli`
//     may import it.
//  3. The agent-free primitives (`internal/ticket`, `internal/stage`,
//     `internal/config`, `internal/userconfig`) must not pull in
//     `internal/agent` or `internal/worktree`.
//  4. `internal/agent` must not import `internal/ticket`,
//     `internal/worktree`, `internal/terminal`, or `internal/cli`.
//  5. `internal/worktree` is a leaf — no internal imports.
//
// When these fail, either fix the design or, if the design has
// genuinely changed, update both the rules here and AGENTS.md in the
// same commit.
package archtest

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "tickets-md"

// pkg is the subset of `go list -json` we care about.
type pkg struct {
	ImportPath string
	Imports    []string
}

// loadPackages shells out to `go list -json ./...` starting from the
// module root (three levels up from this test file). It returns every
// package under the module, with its direct imports.
func loadPackages(t *testing.T) []pkg {
	t.Helper()
	// internal/archtest/arch_test.go → module root is ../../
	root := filepath.Join("..", "..")
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("go list failed: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []pkg
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decoding go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		t.Fatal("go list returned no packages")
	}
	return pkgs
}

// internalPath returns the path relative to the module (e.g.
// "internal/cli") for an import path under this module, or "" if the
// import belongs to another module.
func internalPath(importPath string) string {
	prefix := modulePath + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return ""
	}
	return strings.TrimPrefix(importPath, prefix)
}

// rule encodes a "<from> must not import <forbidden>" constraint.
type rule struct {
	// name is printed in violations.
	name string
	// applies reports whether the rule should be checked against a
	// package at the given module-relative path (e.g. "internal/cli").
	applies func(pkgPath string) bool
	// forbidden reports whether the imported module-relative path is
	// off-limits to packages matched by applies.
	forbidden func(importPath string) bool
}

// hasPrefix is a tiny helper so rule bodies stay declarative.
func hasPrefix(prefix string) func(string) bool {
	return func(s string) bool { return s == prefix || strings.HasPrefix(s, prefix+"/") }
}

// anyOf matches if any of the provided predicates match.
func anyOf(ps ...func(string) bool) func(string) bool {
	return func(s string) bool {
		for _, p := range ps {
			if p(s) {
				return true
			}
		}
		return false
	}
}

// all matches if every provided predicate matches.
func all(ps ...func(string) bool) func(string) bool {
	return func(s string) bool {
		for _, p := range ps {
			if !p(s) {
				return false
			}
		}
		return true
	}
}

// not inverts a predicate.
func not(p func(string) bool) func(string) bool {
	return func(s string) bool { return !p(s) }
}

var rules = []rule{
	{
		name:      "internal/cli is a sink — no other internal package may import it",
		applies:   all(hasPrefix("internal"), not(hasPrefix("internal/cli"))),
		forbidden: hasPrefix("internal/cli"),
	},
	{
		name:      "internal/terminal is private to internal/cli",
		applies:   not(hasPrefix("internal/cli")),
		forbidden: hasPrefix("internal/terminal"),
	},
	{
		name:      "agent-free primitives (ticket/stage/config/userconfig) must not depend on agent/worktree/terminal/cli",
		applies:   anyOf(hasPrefix("internal/ticket"), hasPrefix("internal/stage"), hasPrefix("internal/config"), hasPrefix("internal/userconfig")),
		forbidden: anyOf(hasPrefix("internal/agent"), hasPrefix("internal/worktree"), hasPrefix("internal/terminal"), hasPrefix("internal/cli")),
	},
	{
		name:      "internal/agent must not import ticket/worktree/terminal/cli",
		applies:   hasPrefix("internal/agent"),
		forbidden: anyOf(hasPrefix("internal/ticket"), hasPrefix("internal/worktree"), hasPrefix("internal/terminal"), hasPrefix("internal/cli")),
	},
	{
		name:      "internal/worktree is a leaf — no internal imports",
		applies:   hasPrefix("internal/worktree"),
		forbidden: hasPrefix("internal"),
	},
	{
		name:      "internal/obsidian is a leaf — no internal imports",
		applies:   hasPrefix("internal/obsidian"),
		forbidden: hasPrefix("internal"),
	},
}

func TestLayerRules(t *testing.T) {
	// Override the "applies" predicates so they exempt the rule's own
	// package — a rule about internal/cli shouldn't apply to
	// internal/cli importing internal/cli (same package).
	pkgs := loadPackages(t)

	type violation struct {
		Rule     string
		From     string
		Imported string
	}
	var violations []violation

	for _, p := range pkgs {
		fromInternal := internalPath(p.ImportPath)
		if fromInternal == "" {
			continue
		}
		for _, imp := range p.Imports {
			toInternal := internalPath(imp)
			if toInternal == "" {
				continue
			}
			// A package importing its own module path (can't happen
			// for real, but guard anyway) is not a cross-package
			// import.
			if toInternal == fromInternal {
				continue
			}
			for _, r := range rules {
				if !r.applies(fromInternal) {
					continue
				}
				if !r.forbidden(toInternal) {
					continue
				}
				// Allow a package to "import" itself under a rule's
				// own prefix (e.g. internal/cli subpackages importing
				// each other) — checked above. Anything reaching this
				// point is a real violation.
				violations = append(violations, violation{
					Rule:     r.name,
					From:     p.ImportPath,
					Imported: imp,
				})
			}
		}
	}

	if len(violations) > 0 {
		for _, v := range violations {
			t.Errorf("layer violation: %s\n    %s imports %s", v.Rule, v.From, v.Imported)
		}
	}
}
