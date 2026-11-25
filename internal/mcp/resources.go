package mcp

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/schemas"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// schemaFileSuffix is the suffix of the published schema files; the resource
// name of a schema is its file name without it, the same name `trafico
// schema <nombre>` serves.
const schemaFileSuffix = ".schema.json"

// schemaURI is the scheme+path prefix of the schema resources: one resource
// per published document, read from the embedded FS the binary already
// carries (S5): an agent reads the contract from the protocol, not from the
// repository.
const schemaURI = "trafico://schemas/"

// registerResources publishes the embedded JSON Schemas of schemas/ as MCP
// resources. The documents travel byte for byte the published files: the
// contract lives in schemas/, the MCP layer only serves it.
func registerResources(server *mcp.Server) {
	entries, err := fs.ReadDir(schemas.FS, ".")
	if err != nil {
		// The embedded FS is compiled in; this can only fail if the embed
		// pattern changed elsewhere. It stays an internal error, never a
		// silently empty resources/list.
		panic(fmt.Sprintf("mcp: no se pudo leer el directorio de esquemas embebido: %v", err))
	}
	for _, entry := range entries {
		fileName := entry.Name()
		if !strings.HasSuffix(fileName, schemaFileSuffix) {
			continue
		}
		name := strings.TrimSuffix(fileName, schemaFileSuffix)
		resource := &mcp.Resource{
			URI:         schemaURI + name,
			Name:        name,
			MIMEType:    "application/json",
			Description: fmt.Sprintf("Esquema JSON publicado de la salida --json del comando %s de trafico-cli (fuentes: directorio schemas/ del repositorio).", name),
		}
		server.AddResource(resource, func(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			raw, err := schemas.FS.ReadFile(fileName)
			if err != nil {
				return nil, errs.NewInternal(fmt.Sprintf(
					"no se pudo leer el esquema embebido %q: %v", fileName, err))
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{
						URI:      request.Params.URI,
						MIMEType: "application/json",
						Text:     string(raw),
					},
				},
			}, nil
		})
	}
}
