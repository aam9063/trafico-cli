package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/aam9063/trafico-cli/internal/agentdocs"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPrompts publishes the four recipes of docs/ARRANQUE.md §6.4 as MCP
// prompts (ODD ola-7-mcp S5). The recipes are the embedded data of
// internal/agentdocs — the same single source `trafico skills` serves — so a
// prompt never drifts from the recipe an agent can also read offline. An
// agent discovers them from the protocol, not by digging into the repo.
func registerPrompts(server *mcp.Server) {
	for _, recipe := range agentdocs.Recipes() {
		recipe := recipe
		server.AddPrompt(&mcp.Prompt{
			Name:        recipe.ID,
			Description: recipe.Summary,
		}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{
				Description: recipe.Summary,
				Messages: []*mcp.PromptMessage{
					{
						Role:    mcp.Role("user"),
						Content: &mcp.TextContent{Text: promptText(recipe)},
					},
				},
			}, nil
		})
	}
}

// recipeToolHints maps each recipe id to the tool call of this server that
// reproduces the recipe's CLI command with the same filters. It is MCP
// presentation glue, not business logic: the mapping names tools and
// arguments, never ad-hoc queries.
var recipeToolHints = map[string]string{
	"estado-carretera":    "llama a la herramienta `incidencias` con road y pk_from/pk_to (números, no el rango \"100-140\")",
	"mi-ruta":             "llama a la herramienta `rutas` con el argumento name de la ruta guardada",
	"obras-fin-de-semana": "llama a la herramienta `incidencias` con type, status y from/to (instantes RFC3339)",
	"paneles":             "llama a la herramienta `paneles` con road y direction",
}

// promptText renders one §6.4 recipe as an MCP prompt message: what the
// recipe answers, the tool call that reproduces the CLI command with the same
// filters, and the reading rules, so the agent answers without executing
// shell commands at all.
func promptText(recipe agentdocs.Recipe) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Receta de trafico-cli: %s\n\n", recipe.Summary)
	b.WriteString("Responde a: " + recipe.Answers + "\n\n")
	if hint, ok := recipeToolHints[recipe.ID]; ok {
		b.WriteString("Con las herramientas de este servidor: " + hint + ".\n\n")
	} else {
		// Never silently empty: a recipe without a mapping surfaces its CLI
		// form so the prompt keeps being useable offline.
		b.WriteString("Comando equivalente de la CLI: " + strings.Join(recipe.Command, " ") + "\n\n")
	}
	b.WriteString("Cómo leer la salida: " + recipe.Reading + "\n")
	return b.String()
}
