package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"
)

// updateSpec regenerates docs/cli-spec.md explicitly:
//
//	go test ./internal/cli -run TestCLISpecIsCurrent -update
//
// It is never on by default, so a normal run can only report drift.
var updateSpec = flag.Bool("update", false, "rewrite docs/cli-spec.md")

const specPath = "../../docs/cli-spec.md"

// TestCLISpecIsCurrent is the whole point of generating the reference: a
// hand-maintained document goes stale silently — docs/ARRANQUE.md §6.2 cited a
// command that no longer existed — and this fails the moment the committed
// file stops matching what the binary would print.
func TestCLISpecIsCurrent(t *testing.T) {
	var buf bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&buf)
	root.SetArgs([]string{"docs"})
	if err := root.Execute(); err != nil {
		t.Fatalf("running the docs generator: %v", err)
	}
	got := buf.String()

	if *updateSpec {
		if err := os.WriteFile(specPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", specPath, err)
		}
		t.Logf("wrote %s (%d bytes)", specPath, len(got))
		return
	}

	want, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading %s (regenerate with: go test ./internal/cli -run TestCLISpecIsCurrent -update): %v", specPath, err)
	}
	if got != string(want) {
		t.Errorf("docs/cli-spec.md is stale: it no longer matches the command tree.\nregenerate with: go test ./internal/cli -run TestCLISpecIsCurrent -update")
	}
}

// A hidden command must not appear in the reference: it is not part of the
// interface a user is meant to call.
func TestCLISpecOmitsHiddenCommands(t *testing.T) {
	spec := RenderCLISpec(NewRootCommand())
	if bytes.Contains([]byte(spec), []byte("trafico docs")) {
		t.Error("the generated reference documents the hidden docs command")
	}
	if !bytes.Contains([]byte(spec), []byte("trafico incidents")) {
		t.Error("the generated reference is missing a visible command")
	}
}

// 'trafico version' prints the version and nothing else: it is what a script
// compares and what a reader wants. The commit and the build date are still
// available, behind --json, for a bug report.
func TestVersionTextIsOnlyTheVersion(t *testing.T) {
	code, stdout, _ := runCLIDefaultFormat(t, "version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	got := strings.TrimSpace(stdout.String())
	if got != version {
		t.Errorf("stdout = %q, want %q and nothing else", got, version)
	}
	if strings.ContainsAny(got, "()") || strings.Contains(got, "built") {
		t.Errorf("stdout = %q, want only the version", got)
	}
}

func TestVersionJSONCarriesTheBuildDetail(t *testing.T) {
	code, stdout, _ := runCLI(t, "version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("version --json is not one JSON value: %v", err)
	}
	for _, key := range []string{"version", "commit", "build_date"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("version --json is missing %q", key)
		}
	}
}
