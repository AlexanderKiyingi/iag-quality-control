// Package validation holds the system's validation pack: the user
// requirements and their trace to the automated tests (requirements.json),
// and the check that keeps that trace honest.
package validation

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Requirement is one user requirement and what verifies it.
type Requirement struct {
	ID        string   `json:"id"`
	Area      string   `json:"area"`
	Statement string   `json:"statement"`
	Tests     []string `json:"tests"`
	OQ        []string `json:"oq"`
}

// Pack is requirements.json.
type Pack struct {
	System       string        `json:"system"`
	Note         string        `json:"note"`
	Requirements []Requirement `json:"requirements"`
}

func loadPack(t *testing.T) Pack {
	t.Helper()
	raw, err := os.ReadFile("requirements.json")
	if err != nil {
		t.Fatal(err)
	}
	var p Pack
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("requirements.json: %v", err)
	}
	return p
}

/*
TestRequirementsTraceToTests fails when a requirement names a test that does
not exist (renamed or deleted), has no test at all, or repeats an id — so the
traceability matrix cannot quietly point at nothing.
*/
func TestRequirementsTraceToTests(t *testing.T) {
	p := loadPack(t)
	defined := map[string]bool{}
	re := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == "third_party" || strings.HasPrefix(d.Name(), ".")) && path != ".." {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			defined[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range p.Requirements {
		if seen[r.ID] {
			t.Errorf("%s is listed twice", r.ID)
		}
		seen[r.ID] = true
		if len(r.Tests) == 0 {
			t.Errorf("%s has no test", r.ID)
		}
		for _, name := range r.Tests {
			if !defined[name] {
				t.Errorf("%s names %s, which no longer exists", r.ID, name)
			}
		}
	}
	if len(p.Requirements) < 10 {
		t.Fatalf("only %d requirements", len(p.Requirements))
	}
}
