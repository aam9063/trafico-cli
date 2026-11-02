package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aam9063/trafico-cli/internal/errs"
)

// newSectionsCLIServer serves the real INVIVE and TEFIVA snapshots and
// reports how many requests each path received.
func newSectionsCLIServer(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()
	invive := vmsCLIFixture(t, "../../testdata/feeds/invive/snap-1.xml")
	tefiva := vmsCLIFixture(t, "../../testdata/feeds/tefiva/snap-1.xml")
	counts := make(map[string]int)
	mux := http.NewServeMux()
	mux.HandleFunc("/invive", func(w http.ResponseWriter, r *http.Request) {
		counts["/invive"]++
		w.Write(invive)
	})
	mux.HandleFunc("/tefiva", func(w http.ResponseWriter, r *http.Request) {
		counts["/tefiva"]++
		w.Write(tefiva)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(path string) int { return counts[path] }
}

// writeSectionsCLIConfig writes a config file with both section feeds
// enabled and pointed at the test server.
func writeSectionsCLIConfig(t *testing.T, srvURL string) {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := fmt.Sprintf(`[feeds.dgt-sections-invive]
url = "%s/invive"
enabled = true

[feeds.dgt-sections-animals]
url = "%s/tefiva"
enabled = true
`, srvURL, srvURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
}

// TestSectionsCommandOutputsEnvelope pins `trafico sections --kind animals`
// end to end over the real TEFIVA fixture: exit 0, one JSON envelope on
// stdout with the 3 sections, the shared schema_version, attribution and —
// docs/feeds.md §9 — meta.published_at null, never an invented instant.
func TestSectionsCommandOutputsEnvelope(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newSectionsCLIServer(t)
	writeSectionsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "sections", "--kind", "animals")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if doc["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", doc["schema_version"])
	}
	sections, ok := doc["sections"].([]any)
	if !ok {
		t.Fatalf("the document carries no sections array: %v", doc)
	}
	if len(sections) != 3 {
		t.Fatalf("sections = %d entries, want 3", len(sections))
	}
	first, _ := sections[0].(map[string]any)
	if first["kind"] != "animals" || first["pk_source"] != "reference" {
		t.Errorf("section[0] = %v, want kind animals with pk_source reference", first)
	}
	meta, _ := doc["meta"].(map[string]any)
	if meta == nil {
		t.Fatalf("the document carries no meta block: %v", doc)
	}
	if published, ok := meta["published_at"]; !ok || published != nil {
		t.Errorf("meta.published_at = %v, want null (docs/feeds.md §9)", meta["published_at"])
	}
	if meta["feed"] != "dgt-sections-animals" {
		t.Errorf("meta.feed = %v, want dgt-sections-animals", meta["feed"])
	}
	attribution, _ := doc["attribution"].(map[string]any)
	if attribution == nil || attribution["license"] != "CC BY" {
		t.Errorf("attribution = %v, want the CC BY block", doc["attribution"])
	}
}

// TestSectionsCommandKindIsRequired pins ARRANQUE §6.3: `trafico sections`
// without --kind exits 2 with a usage envelope on stderr and an empty
// stdout, and it spends no request on any feed: there is no sensible
// default between two different datasets.
func TestSectionsCommandKindIsRequired(t *testing.T) {
	setCLIEnv(t)
	srv, counts := newSectionsCLIServer(t)
	writeSectionsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "sections")
	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	doc := decodeJSON(t, stderr)
	errObj, _ := doc["error"].(map[string]any)
	if errObj == nil || errObj["type"] != "usage" {
		t.Errorf("error envelope = %v, want type usage", doc["error"])
	}
	if got := counts("/invive") + counts("/tefiva"); got != 0 {
		t.Errorf("missing kind made %d requests, want 0", got)
	}
}

// TestSectionsCommandUnknownKindIsUsage pins that an unknown --kind exits 2
// without spending a request on any feed.
func TestSectionsCommandUnknownKindIsUsage(t *testing.T) {
	setCLIEnv(t)
	srv, counts := newSectionsCLIServer(t)
	writeSectionsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "sections", "--kind", "motos")
	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	if got := counts("/invive") + counts("/tefiva"); got != 0 {
		t.Errorf("unknown kind made %d requests, want 0", got)
	}
}

// TestSectionsCommandInviveRoadFilter pins `trafico sections --kind invive
// --road cm-220` end to end: the road filter compares normalised names, so
// exactly the CM-220 section of the INVIVE fixture matches.
func TestSectionsCommandInviveRoadFilter(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newSectionsCLIServer(t)
	writeSectionsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "sections", "--kind", "invive", "--road", "cm-220")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	sections, _ := doc["sections"].([]any)
	if len(sections) != 1 {
		t.Fatalf("sections = %d entries, want 1", len(sections))
	}
	section, _ := sections[0].(map[string]any)
	if section["road"] != "CM-220" || section["kind"] != "invive" || section["pk_source"] != "reference" {
		t.Errorf("section = %v, want the CM-220 invive section with pk_source reference", section)
	}
}
