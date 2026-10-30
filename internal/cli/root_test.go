package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandUseIsTrafico(t *testing.T) {
	if got := NewRootCommand().Use; got != "trafico" {
		t.Fatalf("root command Use = %q, want %q", got, "trafico")
	}
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got == "" {
		t.Fatal("version command printed nothing")
	}
}
