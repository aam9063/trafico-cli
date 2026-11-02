package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/errs"
)

// publishedSchemaNames are the two JSON Schema documents of schemas/, the
// published contract (ODD ola-5-agentes S4). Nothing else may appear in the
// list: the schema command serves the contract, not the directory junk.
var publishedSchemaNames = []string{"incident", "incidents-response"}

// TestSchemaListOutputsEnvelope pins `trafico schema` (ODD ola-5-agentes S4
// and S5): exit 0, one JSON envelope on stdout listing every published
// schema with its name, title and description, attribution included.
func TestSchemaListOutputsEnvelope(t *testing.T) {
	code, stdout, stderr := runCLI(t, "schema")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if doc["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", doc["schema_version"])
	}
	if _, ok := doc["attribution"].(map[string]any); !ok {
		t.Error("the document carries no attribution block (P-05)")
	}
	entries, ok := doc["schemas"].([]any)
	if !ok {
		t.Fatalf("the document carries no schemas array: %v", doc)
	}
	if len(entries) != len(publishedSchemaNames) {
		t.Fatalf("schemas = %d entries, want %d", len(entries), len(publishedSchemaNames))
	}
	byName := make(map[string]map[string]any, len(entries))
	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("schema entry is not an object: %v", item)
		}
		name, _ := entry["name"].(string)
		byName[name] = entry
	}
	for _, want := range publishedSchemaNames {
		entry, ok := byName[want]
		if !ok {
			t.Errorf("schema list misses the published schema %q: %v", want, byName)
			continue
		}
		if title, _ := entry["title"].(string); title == "" {
			t.Errorf("schema %q carries no title", want)
		}
		if description, _ := entry["description"].(string); description == "" {
			t.Errorf("schema %q carries no description", want)
		}
	}
}

// TestSchemaGetOutputsTheEmbeddedSchema pins `trafico schema <nombre>`
// (ODD ola-5-agentes S4): the response carries the schema name and the JSON
// Schema document served from the binary (go:embed), not read from disk —
// the released binary does not ship the repository.
func TestSchemaGetOutputsTheEmbeddedSchema(t *testing.T) {
	code, stdout, stderr := runCLI(t, "schema", "incidents-response")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if doc["name"] != "incidents-response" {
		t.Errorf("name = %v, want incidents-response", doc["name"])
	}
	if doc["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", doc["schema_version"])
	}
	if _, ok := doc["attribution"].(map[string]any); !ok {
		t.Error("the document carries no attribution block (P-05)")
	}
	schema, ok := doc["schema"].(map[string]any)
	if !ok {
		t.Fatalf("the document carries no schema object: %v", doc)
	}
	if id, _ := schema["$id"].(string); !strings.Contains(id, "incidents-response.schema.json") {
		t.Errorf("schema.$id = %v, want the published $id of incidents-response", schema["$id"])
	}
}

// TestSchemaDocumentBytesMatchThePublishedFile pins that the served schema
// is byte for byte the published file of schemas/: one single source for
// the contract, never a hand-copied duplicate that can drift.
func TestSchemaDocumentBytesMatchThePublishedFile(t *testing.T) {
	for _, name := range publishedSchemaNames {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, "schema", name)
			if code != errs.ExitSuccess {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
			}
			// Schema is a raw JSON document inside the envelope: decode with
			// json.RawMessage so its bytes survive untouched.
			var doc struct {
				Schema json.RawMessage `json:"schema"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatalf("stdout is not one JSON value: %v\nstdout: %q", err, stdout.String())
			}
			var served bytes.Buffer
			if err := json.Compact(&served, doc.Schema); err != nil {
				t.Fatalf("the served schema is not valid JSON: %v", err)
			}
			published, err := os.ReadFile("../../schemas/" + name + ".schema.json")
			if err != nil {
				t.Fatalf("reading the published schema: %v", err)
			}
			var wanted bytes.Buffer
			if err := json.Compact(&wanted, published); err != nil {
				t.Fatalf("the published schema is not valid JSON: %v", err)
			}
			if !bytes.Equal(served.Bytes(), wanted.Bytes()) {
				t.Error("the served schema differs from the published file of schemas/: the contract must have one single source")
			}
		})
	}
}

// TestSchemaOutputStaysOneLine pins P-01 under --json: the served schema is
// compacted into the envelope, so stdout stays one single-line JSON value
// instead of embedding the multi-line source file.
func TestSchemaOutputStaysOneLine(t *testing.T) {
	code, stdout, _ := runCLI(t, "schema", "incident")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if lines := strings.Count(strings.TrimRight(stdout.String(), "\n"), "\n"); lines != 0 {
		t.Errorf("--json output must be one single line, got %d lines: %.80s...", lines, stdout.String())
	}
}

// TestSchemaListTextRendersOneRowPerSchema pins the human-readable default
// of `trafico schema` (FORMAT-01): one aligned row per published schema
// with its name, title and description, never the indented-JSON fallback.
func TestSchemaListTextRendersOneRowPerSchema(t *testing.T) {
	code, stdout, stderr := runCLIDefaultFormat(t, "schema")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("the text output must not be the JSON fallback: %.120s", out)
	}
	if !strings.Contains(out, "Esquemas publicados (2):") {
		t.Errorf("the heading must announce the two published schemas, got %.120s", out)
	}
	previous := -1
	for _, name := range publishedSchemaNames {
		line := schemaRowLine(out, name)
		if line < 0 {
			t.Errorf("the text output carries no row for the schema %q: %q", name, out)
			continue
		}
		if previous >= 0 && line <= previous {
			t.Errorf("the row of %q is not below the previous row in the list", name)
		}
		previous = line
	}
	summary, err := schemaSummaryOf("incident")
	if err != nil {
		t.Fatalf("reading the embedded summary of incident: %v", err)
	}
	if !strings.Contains(out, summary.Title) {
		t.Errorf("the row of incident misses its title %q", summary.Title)
	}
	if !strings.HasSuffix(out, "Fuente: DGT (CC BY)\n") {
		t.Errorf("the text output must end with the attribution line, got %q", out)
	}
}

// schemaRowLine returns the index of the line that starts the row of the
// schema name (two spaces of indent, then the name), or -1 when absent.
func schemaRowLine(out, name string) int {
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  "+name) {
			return i
		}
	}
	return -1
}

// TestSchemaDocumentTextRendersHeadingAndFormattedJSON pins the
// human-readable default of `trafico schema <nombre>` (FORMAT-01): a
// heading naming the schema and the document as formatted JSON, never the
// indented-JSON envelope fallback.
func TestSchemaDocumentTextRendersHeadingAndFormattedJSON(t *testing.T) {
	code, stdout, stderr := runCLIDefaultFormat(t, "schema", "incidents-response")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.HasPrefix(out, "Esquema: incidents-response\n") {
		t.Errorf("the text output must open with a heading naming the schema, got %.120s", out)
	}
	if strings.Contains(out, "\"schema_version\":1,\"meta\":") {
		t.Errorf("the text output must not be the JSON envelope fallback: %.120s", out)
	}
	// Formatted JSON: the document is indented, not the compact one-liner
	// of the --json path.
	if !strings.Contains(out, "\n  \"$schema\"") {
		t.Errorf("the schema document must be rendered as formatted JSON, got %.200s", out)
	}
	if !strings.HasSuffix(out, "Fuente: DGT (CC BY)\n") {
		t.Errorf("the text output must end with the attribution line, got %q", out)
	}
}

// TestSchemaUnknownNameIsUsage pins ARRANQUE §6.3 via ODD ola-5-agentes S6:
// an unknown schema name is a usage error (exit 2), never an empty list
// with exit 0.
func TestSchemaUnknownNameIsUsage(t *testing.T) {
	code, stdout, stderr := runCLI(t, "schema", "bogus")

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	assertUsageEnvelope(t, stderr.String())
}

// TestSchemaRejectsMoreThanOneName pins that `trafico schema a b` is a
// usage error (exit 2), not a silent list of the first name.
func TestSchemaRejectsMoreThanOneName(t *testing.T) {
	code, stdout, stderr := runCLI(t, "schema", "incident", "incidents-response")

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	assertUsageEnvelope(t, stderr.String())
}

// TestSchemaPrettyEndsWithAttributionLine pins FORMAT-01 for schema: the
// human-readable output ends with the mandatory CC BY closing line.
func TestSchemaPrettyEndsWithAttributionLine(t *testing.T) {
	code, stdout, _ := runCLI(t, "schema", "--pretty")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); !strings.HasSuffix(got, "Fuente: DGT (CC BY)\n") {
		t.Errorf("pretty output must end with the attribution line, got %q", got)
	}
}
