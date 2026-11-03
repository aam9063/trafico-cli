package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/agentdocs"
	"github.com/aam9063/trafico-cli/internal/errs"

	"github.com/spf13/cobra"
)

// expectedRecipeIDs are the four recipes of docs/ARRANQUE.md §6.4
// (ODD ola-5-agentes S3), by their identifiers.
var expectedRecipeIDs = []string{
	"estado-carretera",
	"mi-ruta",
	"obras-fin-de-semana",
	"paneles",
}

// TestSkillsListOutputsEnvelope pins `trafico skills` (ODD ola-5-agentes S1):
// exit 0, one JSON envelope on stdout listing every embedded recipe with its
// identifier and a one-line purpose, attribution included.
func TestSkillsListOutputsEnvelope(t *testing.T) {
	setCLIEnv(t)

	code, stdout, stderr := runCLI(t, "skills")

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
	entries, ok := doc["skills"].([]any)
	if !ok {
		t.Fatalf("the document carries no skills array: %v", doc)
	}
	if len(entries) != len(expectedRecipeIDs) {
		t.Fatalf("skills = %d entries, want %d", len(entries), len(expectedRecipeIDs))
	}
	byID := make(map[string]map[string]any, len(entries))
	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("skill entry is not an object: %v", item)
		}
		id, _ := entry["id"].(string)
		byID[id] = entry
	}
	for _, want := range expectedRecipeIDs {
		entry, ok := byID[want]
		if !ok {
			t.Errorf("skills list misses the recipe %q: %v", want, byID)
			continue
		}
		if summary, _ := entry["summary"].(string); summary == "" {
			t.Errorf("recipe %q carries no one-line purpose", want)
		}
	}
}

// TestSkillsGetOutputsTheFullRecipe pins `trafico skills get <receta>`
// (ODD ola-5-agentes S2): the full recipe, with the exact command, what it
// answers and how to read the output, so an agent with no repository
// context can resolve the case without reading the docs.
func TestSkillsGetOutputsTheFullRecipe(t *testing.T) {
	setCLIEnv(t)

	for _, id := range expectedRecipeIDs {
		t.Run(id, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, "skills", "get", id)
			if code != errs.ExitSuccess {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
			}
			doc := decodeJSON(t, stdout)
			skill, ok := doc["skill"].(map[string]any)
			if !ok {
				t.Fatalf("the document carries no skill object: %v", doc)
			}
			if skill["id"] != id {
				t.Errorf("skill.id = %v, want %q", skill["id"], id)
			}
			command, ok := skill["command"].([]any)
			if !ok || len(command) < 2 {
				t.Fatalf("skill.command = %v, want the exact argv of the recipe", skill["command"])
			}
			if command[0] != "trafico" {
				t.Errorf("skill.command[0] = %v, want trafico", command[0])
			}
			for _, field := range []string{"summary", "answers", "reading"} {
				if value, _ := skill[field].(string); value == "" {
					t.Errorf("skill.%s is empty; the recipe must be self-contained", field)
				}
			}
			if _, ok := doc["attribution"].(map[string]any); !ok {
				t.Error("the document carries no attribution block (P-05)")
			}
		})
	}
}

// TestSkillsListTextRendersOneRowPerRecipe pins the human-readable default
// of `trafico skills` (FORMAT-01): one aligned row per recipe with its
// identifier and one-line purpose, never the indented-JSON fallback.
func TestSkillsListTextRendersOneRowPerRecipe(t *testing.T) {
	setCLIEnv(t)

	code, stdout, stderr := runCLIDefaultFormat(t, "skills")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("the text output must not be the JSON fallback: %.120s", out)
	}
	if !strings.Contains(out, "Recetas para agentes (4):") {
		t.Errorf("the heading must announce the four recipes, got %.120s", out)
	}
	previous := -1
	for _, id := range expectedRecipeIDs {
		line := rowLine(out, id)
		if line < 0 {
			t.Errorf("the text output carries no row for the recipe %q: %q", id, out)
			continue
		}
		if previous >= 0 && line <= previous {
			t.Errorf("the row of %q is not below the previous row in the list", id)
		}
		previous = line
	}
	for _, id := range expectedRecipeIDs {
		recipe, ok := agentdocs.Get(id)
		if !ok || recipe.Summary == "" {
			continue
		}
		if !strings.Contains(out, recipe.Summary) {
			t.Errorf("the row of %q misses its one-line purpose %q", id, recipe.Summary)
		}
	}
	if !strings.HasSuffix(out, "Fuente: DGT (CC BY)\n") {
		t.Errorf("the text output must end with the attribution line, got %q", out)
	}
}

// rowLine returns the index of the line that starts the row of the recipe
// id (two spaces of indent, then the identifier), or -1 when absent.
func rowLine(out, id string) int {
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  "+id) {
			return i
		}
	}
	return -1
}

// TestSkillsGetTextRendersLabelledFields pins the human-readable default of
// `trafico skills get <receta>` (FORMAT-01): labelled fields, the exact
// command on its own separable lines, never the indented-JSON fallback.
func TestSkillsGetTextRendersLabelledFields(t *testing.T) {
	setCLIEnv(t)

	code, stdout, stderr := runCLIDefaultFormat(t, "skills", "get", "mi-ruta")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("the text output must not be the JSON fallback: %.120s", out)
	}
	recipe, ok := agentdocs.Get("mi-ruta")
	if !ok {
		t.Fatal("the embedded recipes lost mi-ruta")
	}
	for _, want := range []string{
		"Receta: mi-ruta",
		"Resumen: " + recipe.Summary,
		"Comando:",
		"  " + strings.Join(recipe.Command, " "),
		"Responde a:",
		"Cómo leer la salida:",
		recipe.Answers,
		recipe.Reading,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the text output misses %q; output:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "Fuente: DGT (CC BY)\n") {
		t.Errorf("the text output must end with the attribution line, got %q", out)
	}
}

// TestSkillsGetUnknownRecipeIsUsage pins ARRANQUE §6.3 via ODD ola-5-agentes
// S6: an unknown recipe id is a usage error (exit 2), never an empty list
// with exit 0.
func TestSkillsGetUnknownRecipeIsUsage(t *testing.T) {
	setCLIEnv(t)

	code, stdout, stderr := runCLI(t, "skills", "get", "bogus")

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	assertUsageEnvelope(t, stderr.String())
}

// TestSkillsGetWithoutNameIsUsage pins that `trafico skills get` without a
// recipe id is a usage error (exit 2), not a silent empty render.
func TestSkillsGetWithoutNameIsUsage(t *testing.T) {
	setCLIEnv(t)

	code, stdout, stderr := runCLI(t, "skills", "get")

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	assertUsageEnvelope(t, stderr.String())
}

// TestSkillsUnknownSubcommandIsUsage pins that `trafico skills bogus` is a
// usage error (exit 2), like every other command of the tree.
func TestSkillsUnknownSubcommandIsUsage(t *testing.T) {
	setCLIEnv(t)

	code, stdout, stderr := runCLI(t, "skills", "bogus")

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	assertUsageEnvelope(t, stderr.String())
}

// TestSkillsPrettyEndsWithAttributionLine pins FORMAT-01 for skills: the
// human-readable output ends with the mandatory CC BY closing line.
func TestSkillsPrettyEndsWithAttributionLine(t *testing.T) {
	setCLIEnv(t)

	code, stdout, _ := runCLI(t, "skills", "--pretty")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); !strings.HasSuffix(got, "Fuente: DGT (CC BY)\n") {
		t.Errorf("pretty output must end with the attribution line, got %q", got)
	}
}

// TestRecipesMatchCommandTree walks the cobra command tree and asserts that
// every command path and flag named by an embedded recipe actually exists
// (ODD ola-5-agentes T1). A recipe that points at a flag a later rename
// removed would send an agent into an exit 2 with no idea why, so the
// recipes are validated against the live command tree, not against the
// documentation.
func TestRecipesMatchCommandTree(t *testing.T) {
	root := NewRootCommand()
	seen := make(map[string]bool)
	for _, recipe := range agentdocs.Recipes() {
		t.Run(recipe.ID, func(t *testing.T) {
			if seen[recipe.ID] {
				t.Errorf("duplicate recipe id %q", recipe.ID)
			}
			seen[recipe.ID] = true
			if recipe.Summary == "" || recipe.Answers == "" || recipe.Reading == "" {
				t.Error("summary, answers and reading must all be filled in")
			}
			if len(recipe.Command) < 2 || recipe.Command[0] != "trafico" {
				t.Fatalf("command must be the exact argv starting with trafico, got %v", recipe.Command)
			}
			argv := recipe.Command[1:]
			// Cobra itself must be able to route the command path: an
			// unknown command would exit 2 before any flag is parsed.
			found, _, err := root.Find(argv)
			if err != nil {
				t.Fatalf("cobra cannot route %v: %v", argv, err)
			}
			if found == root {
				t.Fatalf("the recipe names no subcommand: %v", argv)
			}
			// The subcommand chain: every non-flag token until the first
			// flag. A token that names no subcommand is a positional
			// argument (a route name, a date placeholder) and ends the
			// chain.
			cmd := root
			i := 0
			for ; i < len(argv) && !strings.HasPrefix(argv[i], "-"); i++ {
				next := findSubcommand(cmd, argv[i])
				if next == nil {
					break
				}
				cmd = next
			}
			// Flags: every --name the recipe passes must exist on the leaf
			// command, and a flag that takes a value consumes the next
			// token so values are never mistaken for flags.
			flags := cmd.Flags()
			for ; i < len(argv); i++ {
				tok := argv[i]
				if !strings.HasPrefix(tok, "--") {
					continue // positional argument or flag value
				}
				name := strings.TrimPrefix(tok, "--")
				if eq := strings.Index(name, "="); eq >= 0 {
					name = name[:eq]
				}
				flag := flags.Lookup(name)
				if flag == nil {
					t.Errorf("recipe names flag --%s, but %q has no such flag", name, cmd.CommandPath())
					continue
				}
				if flag.Value.Type() != "bool" && i+1 < len(argv) {
					i++ // the next token is this flag's value
				}
			}
		})
	}
	if len(seen) != len(expectedRecipeIDs) {
		t.Errorf("agentdocs exposes %d recipes, want the %d of docs/ARRANQUE.md §6.4", len(seen), len(expectedRecipeIDs))
	}
	for _, want := range expectedRecipeIDs {
		if !seen[want] {
			t.Errorf("agentdocs misses the recipe %q of docs/ARRANQUE.md §6.4", want)
		}
	}
}

// assertUsageEnvelope reports whether stderr carries exactly one JSON error
// envelope with the usage type and the exit code 2 of ARRANQUE §6.3.
func assertUsageEnvelope(t *testing.T, stderr string) {
	t.Helper()
	doc := decodeJSONString(t, stderr)
	errObj, _ := doc["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("stderr carries no error envelope: %v", doc)
	}
	if errObj["type"] != "usage" {
		t.Errorf("error.type = %v, want usage", errObj["type"])
	}
	if errObj["exit_code"] != float64(errs.ExitUsage) {
		t.Errorf("error.exit_code = %v, want 2", errObj["exit_code"])
	}
}

// decodeJSONString parses one JSON document from a string.
func decodeJSONString(t *testing.T, raw string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("payload is not one JSON value: %v\npayload: %q", err, raw)
	}
	return doc
}

// findSubcommand returns the subcommand of cmd named name, or nil.
func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}
