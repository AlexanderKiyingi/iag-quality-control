// Command validation-pack writes the traceability matrix: each user
// requirement (validation/requirements.json), the tests that verify it, and
// — given a `go test -json` run — whether each passed.
//
//	QC_TEST_DATABASE_URL=… go test -json ./... > test-run.json
//	go run ./cmd/validation-pack -run test-run.json > traceability.md
//
// Without -run the matrix is printed with the result column empty, as the
// plan of what an execution must show.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type requirement struct {
	ID        string   `json:"id"`
	Area      string   `json:"area"`
	Statement string   `json:"statement"`
	Tests     []string `json:"tests"`
	OQ        []string `json:"oq"`
}

type pack struct {
	System       string        `json:"system"`
	Note         string        `json:"note"`
	Requirements []requirement `json:"requirements"`
}

type event struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
}

func main() {
	reqPath := flag.String("requirements", "validation/requirements.json", "requirements file")
	runPath := flag.String("run", "", "go test -json output")
	flag.Parse()
	raw, err := os.ReadFile(*reqPath)
	if err != nil {
		fail(err)
	}
	var p pack
	if err := json.Unmarshal(raw, &p); err != nil {
		fail(err)
	}
	results := map[string]string{} // top-level test → pass / fail / skip
	if *runPath != "" {
		f, err := os.Open(*runPath)
		if err != nil {
			fail(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.Test == "" || strings.Contains(e.Test, "/") {
				continue
			}
			switch e.Action {
			case "pass", "fail", "skip":
				results[e.Test] = e.Action
			}
		}
		_ = f.Close()
	}
	fmt.Printf("# Traceability matrix — %s\n\n", p.System)
	fmt.Printf("Generated %s. %s\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"), p.Note)
	fmt.Println("| Requirement | Area | Statement | Verified by | Result | OQ script |")
	fmt.Println("|---|---|---|---|---|---|")
	verified, total := 0, 0
	for _, r := range p.Requirements {
		total++
		status := "not run"
		if *runPath != "" {
			status = verdict(r.Tests, results)
			if status == "pass" {
				verified++
			}
		}
		tests := append([]string(nil), r.Tests...)
		sort.Strings(tests)
		fmt.Printf("| %s | %s | %s | %s | %s | %s |\n", r.ID, r.Area, r.Statement, strings.Join(tests, "<br>"), status, strings.Join(r.OQ, ", "))
	}
	if *runPath != "" {
		fmt.Printf("\n**%d of %d requirements verified** by this run. A skipped test (database tests without QC_TEST_DATABASE_URL) does not verify its requirement.\n", verified, total)
	}
}

// verdict is pass only when every test ran and passed.
func verdict(tests []string, results map[string]string) string {
	out := "pass"
	for _, t := range tests {
		switch results[t] {
		case "pass":
		case "fail":
			return "FAIL (" + t + ")"
		case "skip":
			out = "incomplete (" + t + " skipped)"
		default:
			if out == "pass" {
				out = "incomplete (" + t + " not run)"
			}
		}
	}
	return out
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "validation-pack:", err)
	os.Exit(1)
}
