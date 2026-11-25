package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Contract note (S4): every handler below marshals the app response exactly
// as the command's --json does — the same struct, so the same keys, the same
// attribution and the same meta.published_at — and carries it in ONE
// mcp.TextContent. StructuredContent stays unset: two serialisations of the
// same answer are two formats, and an agent must learn one.

// incidenciasArgs is the argument contract of the `incidencias` tool, the
// same filters as `trafico incidents` (docs/ARRANQUE.md §5.2). Dates are
// instants in RFC3339 (the app layer consumes instants, ODD ola-7-mcp S3:
// the MCP layer adds no date logic of its own); pk_from/pk_to are numbers,
// not the "100-140" range string of the flag, for the same reason.
type incidenciasArgs struct {
	Road      []string `json:"road,omitempty"`
	PkFrom    *float64 `json:"pk_from,omitempty"`
	PkTo      *float64 `json:"pk_to,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Province  []string `json:"province,omitempty"`
	Type      []string `json:"type,omitempty"`
	Severity  []string `json:"severity,omitempty"`
	Status    string   `json:"status,omitempty"`
	From      *string  `json:"from,omitempty"`
	To        *string  `json:"to,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Sort      string   `json:"sort,omitempty"`
	Near      string   `json:"near,omitempty"`
	RadiusKm  *float64 `json:"radius_km,omitempty"`
}

// criteria translates the tool arguments into app.IncidentCriteria. Every
// cross-field rule (PK needs exactly one road, negative limit, unknown
// direction) still lives in internal/filter and internal/app: the
// translation adds none of its own (S3).
func (a incidenciasArgs) criteria() (app.IncidentCriteria, error) {
	criteria := app.IncidentCriteria{
		Criteria: filter.Criteria{
			Roads:      a.Road,
			Direction:  a.Direction,
			Provinces:  a.Province,
			Types:      a.Type,
			Severities: a.Severity,
			Status:     a.Status,
			Limit:      a.Limit,
			Sort:       a.Sort,
		},
		Near: a.Near,
	}
	if a.PkFrom != nil || a.PkTo != nil {
		criteria.PK = &filter.Range{From: a.PkFrom, To: a.PkTo}
	}
	if a.RadiusKm != nil {
		criteria.RadiusKm = *a.RadiusKm
	}
	if a.From != nil {
		from, err := parseInstant("from", *a.From)
		if err != nil {
			return app.IncidentCriteria{}, err
		}
		criteria.From = from
	}
	if a.To != nil {
		to, err := parseInstant("to", *a.To)
		if err != nil {
			return app.IncidentCriteria{}, err
		}
		criteria.To = to
	}
	return criteria, nil
}

// staticArgsShared collects the filters of signs, radars and sections, which
// share the reduced filter set of docs/feeds.md §5–§8 (no PK, no type, no
// severity: the feeds publish none of them).
type staticArgsShared struct {
	Road      []string `json:"road,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Province  []string `json:"province,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// tramosArgs is the argument contract of the `tramos` tool: the dataset
// selector plus the filters signs, radars and sections share.
type tramosArgs struct {
	// Kind selects the dataset: invive or animals. It is required (S3: the
	// rule stays in app.Sections).
	Kind string `json:"kind"`
	// Road, Direction, Province and Limit are the filters the static feeds
	// share.
	Road      []string `json:"road,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Province  []string `json:"province,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// rutasArgs is the argument contract of the `rutas` tool. An empty Name lists
// every saved route; a set Name checks that route against the feed.
type rutasArgs struct {
	Name string `json:"name,omitempty"`
}

// estadoFeedsArgs is the argument contract of the `estado_feeds` tool.
type estadoFeedsArgs struct {
	// Check fetches every enabled feed; false lists state only, no request.
	Check bool `json:"check,omitempty"`
}

// parseInstant parses the RFC3339 instant of a date-window argument. The
// message names the argument and a full example, so the agent can fix the
// call itself (the same reason the flags carry the format in their help).
func parseInstant(name, value string) (*time.Time, error) {
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errs.NewUsage(fmt.Sprintf(
			"el argumento %q debe ser un instante RFC3339, por ejemplo 2026-01-17T00:00:00Z (recibido %q)",
			name, value))
	}
	return &instant, nil
}

// incidenciasToolDefinition declares the `incidencias` tool.
func incidenciasToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: "incidencias",
		Description: "Consulta las incidencias y obras de la DGT por carretera, punto " +
			"kilométrico, provincia, severidad, tipo o radio alrededor de un lugar guardado " +
			"de la configuración (near + radius_km). Devuelve el mismo contrato JSON que " +
			"`trafico incidents --json`, con attribution y meta.published_at.",
		Register: func(server *mcp.Server, deps Deps) {
			mcp.AddTool(server, readOnlyTool("incidencias", "Incidencias y obras de la DGT por carretera, pk, provincia, severidad, tipo o radio"), func(
				ctx context.Context, _ *mcp.CallToolRequest, args incidenciasArgs,
			) (*mcp.CallToolResult, any, error) {
				criteria, err := args.criteria()
				if err != nil {
					return nil, nil, err
				}
				response, err := app.Incidents(ctx, deps.Config, deps.Fetcher, criteria)
				if err != nil {
					return nil, nil, err
				}
				writeWarning(deps, response.Warning)
				return contractResult(response)
			})
		},
	}
}

// panelesToolDefinition declares the `paneles` tool (app.Signs).
func panelesToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: "paneles",
		Description: "Consulta los mensajes que muestran los paneles variable de la DGT, " +
			"por carretera, provincia o dirección. Devuelve el mismo contrato JSON que " +
			"`trafico signs --json`, con attribution y meta.published_at.",
		Register: func(server *mcp.Server, deps Deps) {
			mcp.AddTool(server, readOnlyTool("paneles", "Mensajes de los paneles variable de la DGT"), func(
				ctx context.Context, _ *mcp.CallToolRequest, args staticArgsShared,
			) (*mcp.CallToolResult, any, error) {
				response, err := app.Signs(ctx, deps.Config, deps.Fetcher, app.SignCriteria{
					Roads:     args.Road,
					Direction: args.Direction,
					Provinces: args.Province,
					Limit:     args.Limit,
				})
				if err != nil {
					return nil, nil, err
				}
				writeWarning(deps, response.Warning)
				return contractResult(response)
			})
		},
	}
}

// radaresToolDefinition declares the `radares` tool (app.Radars).
func radaresToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: "radares",
		Description: "Consulta los radares fijos de la DGT, por carretera, provincia o " +
			"dirección. Devuelve el mismo contrato JSON que `trafico radars --json`: para " +
			"este feed meta.published_at es null, porque el feed no publica fecha (docs/feeds.md §9).",
		Register: func(server *mcp.Server, deps Deps) {
			mcp.AddTool(server, readOnlyTool("radares", "Radares fijos de la DGT"), func(
				ctx context.Context, _ *mcp.CallToolRequest, args staticArgsShared,
			) (*mcp.CallToolResult, any, error) {
				response, err := app.Radars(ctx, deps.Config, deps.Fetcher, app.RadarCriteria{
					Roads:     args.Road,
					Direction: args.Direction,
					Provinces: args.Province,
					Limit:     args.Limit,
				})
				if err != nil {
					return nil, nil, err
				}
				writeWarning(deps, response.Warning)
				return contractResult(response)
			})
		},
	}
}

// tramosToolDefinition declares the `tramos` tool (app.Sections). Kind selects
// the dataset exactly like `--kind`: invive or animals, no default.
func tramosToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: "tramos",
		Description: "Consulta los tramos de la DGT, por carretera, provincia o dirección. " +
			"El argumento kind es obligatorio y elige el dataset: invive (inventario de " +
			"tramos) o animals (puntos negros de colisiones con animales). Devuelve el mismo " +
			"contrato JSON que `trafico sections --kind <kind> --json`, con attribution y meta.published_at.",
		Register: func(server *mcp.Server, deps Deps) {
			mcp.AddTool(server, readOnlyTool("tramos", "Tramos de la DGT: invive (inventario) o animals (colisiones con animales)"), func(
				ctx context.Context, _ *mcp.CallToolRequest, args tramosArgs,
			) (*mcp.CallToolResult, any, error) {
				response, err := app.Sections(ctx, deps.Config, deps.Fetcher, app.SectionCriteria{
					Kind:      args.Kind,
					Roads:     args.Road,
					Direction: args.Direction,
					Provinces: args.Province,
					Limit:     args.Limit,
				})
				if err != nil {
					return nil, nil, err
				}
				writeWarning(deps, response.Warning)
				return contractResult(response)
			})
		},
	}
}

// rutasToolDefinition declares the `rutas` tool: without a name it lists
// every saved route (the `trafico route list --json` contract), with a name
// it checks the route against the feed (the `trafico route check --json`
// contract). Both are READS of the saved routes: adding, editing or removing
// a route is a write the v1 registry deliberately does not expose (S2) — the
// extension point app.AddRoute/app.RemoveRoute.
func rutasToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: "rutas",
		Description: "Consulta las rutas guardadas en la configuración: sin name devuelve " +
			"todas con sus tramos (contrato de `trafico route list --json`); con name " +
			"comprueba las incidencias activas en cada tramo de esa ruta (contrato de " +
			"`trafico route check --json`), con summary. Solo lectura.",
		Register: func(server *mcp.Server, deps Deps) {
			mcp.AddTool(server, readOnlyTool("rutas", "Lista las rutas guardadas, o comprueba las incidencias de una"), func(
				ctx context.Context, _ *mcp.CallToolRequest, args rutasArgs,
			) (*mcp.CallToolResult, any, error) {
				if args.Name == "" {
					routes, err := app.Routes(deps.Config)
					if err != nil {
						return nil, nil, err
					}
					meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: len(routes)}
					response := struct {
						output.Envelope
						Routes []app.RouteInfo `json:"routes"`
					}{
						Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
						Routes:   routes,
					}
					return contractResult(response)
				}
				response, err := app.RouteCheck(ctx, deps.Config, deps.Fetcher, args.Name)
				if err != nil {
					return nil, nil, err
				}
				writeWarning(deps, response.Warning)
				return contractResult(response)
			})
		},
	}
}

// estadoFeedsToolDefinition declares the `estado_feeds` tool: the per-feed
// health of the configuration. With check false it lists state and cache
// metadata with no request at all (app.FeedsList); with check true it fetches
// every enabled feed and reports freshness and errors (app.FeedsStatus).
func estadoFeedsToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: "estado_feeds",
		Description: "Estado de los feeds configurados: enabled, disabled, ok o error, con " +
			"la última descarga y la fecha de publicación de cada uno. Con check false " +
			"(por defecto) no hace ninguna petición: lee la configuración y la caché local. " +
			"Con check true descarga cada feed activo y comprueba que responde.",
		Register: func(server *mcp.Server, deps Deps) {
			mcp.AddTool(server, readOnlyTool("estado_feeds", "Estado de los feeds: enabled/disabled/ok/error, con frescura y caché"), func(
				ctx context.Context, _ *mcp.CallToolRequest, args estadoFeedsArgs,
			) (*mcp.CallToolResult, any, error) {
				var list []app.FeedStatus
				var err error
				if args.Check {
					list, err = app.FeedsStatus(ctx, deps.Config, deps.Fetcher)
				} else {
					list, err = app.FeedsList(ctx, deps.Config, deps.Fetcher)
				}
				if err != nil {
					return nil, nil, err
				}
				for _, status := range list {
					if status.Warning != nil {
						writeWarning(deps, *status.Warning)
					}
				}
				// The envelope is the one `trafico feeds --json` prints: the
				// aggregation stays owned by app.DeriveMeta.
				now := time.Now().UTC()
				response := struct {
					output.Envelope
					Feeds []app.FeedStatus `json:"feeds"`
				}{
					Envelope: output.NewEnvelope(app.DeriveMeta(list, now, args.Check), output.NewAttribution(now)),
					Feeds:    list,
				}
				return contractResult(response)
			})
		},
	}
}

// contractResult serialises a response of the --json contract into one
// TextContent (S4). Marshal failures are internal, never a silent empty answer.
func contractResult(response any) (*mcp.CallToolResult, any, error) {
	raw, err := json.Marshal(response)
	if err != nil {
		return nil, nil, errs.NewInternal(fmt.Sprintf(
			"no se pudo serializar la respuesta al contrato JSON: %v", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, nil, nil
}

// writeWarning puts a cache-fallback warning on the process stderr, where the
// commands put them. stdout is the MCP transport channel (P-01): a warning
// there would corrupt the protocol stream.
func writeWarning(deps Deps, warning string) {
	if deps.Diag == nil || warning == "" {
		return
	}
	fmt.Fprintf(deps.Diag, "trafico mcp: %s\n", warning)
}
