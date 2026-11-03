package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/agentdocs"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
)

// skillsListResponse is the JSON contract of `trafico skills` (ODD
// ola-5-agentes S1): the shared envelope with one line per embedded recipe.
type skillsListResponse struct {
	output.Envelope
	Skills []skillSummary `json:"skills"`
}

// skillSummary is one entry of the skills list: the identifier and the
// one-line purpose of a recipe.
type skillSummary struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// skillDetailResponse is the JSON contract of `trafico skills get <receta>`
// (ODD ola-5-agentes S2): the full recipe, self-contained enough for an
// agent with no repository context.
type skillDetailResponse struct {
	output.Envelope
	Skill agentdocs.Recipe `json:"skill"`
}

// newSkillsCommand builds `trafico skills`, the recipe list of the embedded
// agent skill (docs/ARRANQUE.md §6.4), with its `get <receta>` subcommand.
// Both serve local data: no feed is touched and no network request is made.
func newSkillsCommand() *cobra.Command {
	skillsCmd := &cobra.Command{
		Use:   "skills",
		Short: "List the embedded recipes for AI agents (text by default, --json for one JSON value)",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico skills bogus` is an invalid argument (exit 2,
		// ARRANQUE §6.3) instead of a silent help print (exit 0).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		RunE: runSkillsList,
	}
	skillsCmd.AddCommand(newSkillsGetCommand())
	addPrettyFlag(skillsCmd)
	return skillsCmd
}

// newSkillsGetCommand builds `trafico skills get <receta>`: the full recipe
// by its identifier. An unknown recipe is a usage error (exit 2), never an
// empty render with exit 0 (ODD ola-5-agentes S6).
func newSkillsGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <receta>",
		Short: "Show one recipe: the exact command, what it answers and how to read the output",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el id de una receta: trafico skills get <receta>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			recipe, ok := agentdocs.Get(args[0])
			if !ok {
				return errs.NewUsage(fmt.Sprintf(
					"no existe la receta %q; las recetas disponibles son: %s",
					args[0], strings.Join(recipeIDList(), ", ")))
			}
			meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: 1}
			response := skillDetailResponse{
				Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
				Skill:    recipe,
			}
			pretty := prettyFor(cmd)
			return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty, PrettyFunc: renderSkillDetailText})
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// runSkillsList answers `trafico skills`.
func runSkillsList(cmd *cobra.Command, args []string) error {
	recipes := agentdocs.Recipes()
	summaries := make([]skillSummary, 0, len(recipes))
	for _, recipe := range recipes {
		summaries = append(summaries, skillSummary{ID: recipe.ID, Summary: recipe.Summary})
	}
	meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: len(summaries)}
	response := skillsListResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Skills:   summaries,
	}
	pretty := prettyFor(cmd)
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty, PrettyFunc: renderSkillsListText})
}

// renderSkillsListText renders `trafico skills` for a human (FORMAT-01):
// a heading with the recipe count and one aligned row per recipe with its
// identifier and one-line purpose, closed by the attribution line
// (ARRANQUE section 2.1). No ANSI (ARRANQUE section 6.1).
func renderSkillsListText(w io.Writer, v any) error {
	response, ok := v.(skillsListResponse)
	if !ok {
		return errs.NewInternal("el render de texto de skills recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Recetas para agentes (%d):\n\n", len(response.Skills))
	width := 0
	for _, skill := range response.Skills {
		if l := len(skill.ID); l > width {
			width = l
		}
	}
	for _, skill := range response.Skills {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, skill.ID, skill.Summary)
	}
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderSkillDetailText renders `trafico skills get <receta>` for a human
// (FORMAT-01): labelled fields in Spanish, with the exact command on its
// own indented line so a user can copy it into a shell without digging it
// out of prose, closed by the attribution line (ARRANQUE section 2.1).
func renderSkillDetailText(w io.Writer, v any) error {
	response, ok := v.(skillDetailResponse)
	if !ok {
		return errs.NewInternal("el render de texto de skills get recibió una respuesta de otro tipo")
	}
	recipe := response.Skill
	var b strings.Builder
	fmt.Fprintf(&b, "Receta: %s\n", recipe.ID)
	fmt.Fprintf(&b, "Resumen: %s\n", recipe.Summary)
	fmt.Fprintf(&b, "\nComando:\n  %s\n", strings.Join(recipe.Command, " "))
	fmt.Fprintf(&b, "\nResponde a:\n  %s\n", recipe.Answers)
	fmt.Fprintf(&b, "\nCómo leer la salida:\n  %s\n", recipe.Reading)
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}
func recipeIDList() []string {
	recipes := agentdocs.Recipes()
	ids := make([]string, 0, len(recipes))
	for _, recipe := range recipes {
		ids = append(ids, recipe.ID)
	}
	return ids
}
