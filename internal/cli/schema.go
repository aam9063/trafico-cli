package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/output"
	"github.com/aam9063/trafico-cli/schemas"

	"github.com/spf13/cobra"
)

// schemaListResponse is the JSON contract of `trafico schema` (ODD
// ola-5-agentes S4): the shared envelope with one entry per published
// schema.
type schemaListResponse struct {
	output.Envelope
	Schemas []schemaSummary `json:"schemas"`
}

// schemaSummary is one entry of the schema list: the command name, the
// title and the description of the published document.
type schemaSummary struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// schemaDocumentResponse is the JSON contract of `trafico schema <nombre>`
// (ODD ola-5-agentes S4): schema carries the JSON Schema document served
// from the binary (go:embed), never read from schemas/ on disk — the
// binary installed by a release ships without the repository.
type schemaDocumentResponse struct {
	output.Envelope
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}

// schemaFileName is the suffix every published schema file carries; the
// command name of a schema is its file name without it.
const schemaFileName = ".schema.json"

// newSchemaCommand builds `trafico schema [<nombre>]`: without a name it
// lists the published schemas, with one it serves the embedded document.
// An unknown name is a usage error (exit 2), never an empty list with exit
// 0 (ODD ola-5-agentes S6).
func newSchemaCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema [<nombre>]",
		Short: "List the published JSON Schemas, or print one (text by default, --json for one JSON value)",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return errs.NewUsage("indica como máximo un nombre de esquema: trafico schema [<nombre>]")
			}
			return nil
		},
		RunE: runSchema,
	}
	addPrettyFlag(cmd)
	return cmd
}

// runSchema answers `trafico schema [<nombre>]`.
func runSchema(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return writeSchemaList(cmd)
	}
	return writeSchemaDocument(cmd, args[0])
}

// writeSchemaList renders the list of published schemas.
func writeSchemaList(cmd *cobra.Command) error {
	names, err := schemaNames()
	if err != nil {
		return err
	}
	summaries := make([]schemaSummary, 0, len(names))
	for _, name := range names {
		summary, err := schemaSummaryOf(name)
		if err != nil {
			return err
		}
		summaries = append(summaries, summary)
	}
	meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: len(summaries)}
	response := schemaListResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Schemas:  summaries,
	}
	pretty := prettyFor(cmd)
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// writeSchemaDocument renders one embedded schema document. The document is
// compacted into the envelope, so `--json` stays one single-line JSON value
// (P-01) while the content remains byte for byte the published file.
func writeSchemaDocument(cmd *cobra.Command, name string) error {
	names, err := schemaNames()
	if err != nil {
		return err
	}
	raw, readErr := schemas.FS.ReadFile(name + schemaFileName)
	if readErr != nil {
		// The contract lives in the binary: an unknown name is invalid
		// arguments (exit 2), not a not-found lookup of the filesystem.
		return errs.NewUsage(fmt.Sprintf(
			"no existe el esquema %q; los esquemas publicados son: %s",
			name, strings.Join(names, ", ")))
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return errs.NewInternal(fmt.Sprintf("el esquema embebido %q no es JSON válido: %v", name, err))
	}
	meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: 1}
	response := schemaDocumentResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Name:     name,
		Schema:   compact.Bytes(),
	}
	pretty := prettyFor(cmd)
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// schemaNames returns the command names of every embedded schema, sorted as
// the embed FS orders its directory entries (lexicographically).
func schemaNames() ([]string, error) {
	entries, err := fs.ReadDir(schemas.FS, ".")
	if err != nil {
		return nil, errs.NewInternal(fmt.Sprintf("no se pudo leer el directorio de esquemas embebido: %v", err))
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if fileName := entry.Name(); strings.HasSuffix(fileName, schemaFileName) {
			names = append(names, strings.TrimSuffix(fileName, schemaFileName))
		}
	}
	return names, nil
}

// schemaSummaryOf extracts the title and description of one embedded
// schema document for the list. A schema without them is a broken contract
// and stays an internal error, not a silently empty row.
func schemaSummaryOf(name string) (schemaSummary, error) {
	raw, err := schemas.FS.ReadFile(name + schemaFileName)
	if err != nil {
		return schemaSummary{}, errs.NewInternal(fmt.Sprintf("no se pudo leer el esquema embebido %q: %v", name, err))
	}
	var doc struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return schemaSummary{}, errs.NewInternal(fmt.Sprintf("el esquema embebido %q no es JSON válido: %v", name, err))
	}
	return schemaSummary{Name: name, Title: doc.Title, Description: doc.Description}, nil
}
