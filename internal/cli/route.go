package cli

import (
	"fmt"
	"time"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
)

// routesResponse is the JSON contract of `trafico route add/list/remove`
// (ROUTE-01): the shared envelope with the full saved-route list, so a
// mutation shows its result and list stays a plain query.
type routesResponse struct {
	output.Envelope
	Routes []app.RouteInfo `json:"routes"`
}

// routeResponse is the JSON contract of `trafico route show <name>`.
type routeResponse struct {
	output.Envelope
	Route app.RouteInfo `json:"route"`
}

// newRouteCommand builds `trafico route` with its add, list, show, remove
// and check subcommands (docs/ARRANQUE.md §5.1, §5.4). add/list/show/remove
// only touch the local config; check fetches the situations feed exactly
// once for the whole route.
func newRouteCommand() *cobra.Command {
	routeCmd := &cobra.Command{
		Use:   "route",
		Short: "Manage saved routes of road segments and check them against the feed",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico route bogus` is an invalid argument (exit 2,
		// ARRANQUE §6.3) instead of a silent help print (exit 0).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return errs.NewUsage("indica un subcomando: add, list, show, remove o check")
		},
	}
	routeCmd.AddCommand(
		newRouteAddCommand(),
		newRouteListCommand(),
		newRouteShowCommand(),
		newRouteRemoveCommand(),
		newRouteCheckCommand(),
	)
	return routeCmd
}

// newRouteAddCommand builds `trafico route add <name> --segment ROAD:FROM-TO
// [--segment ...] [--direction both]`.
func newRouteAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Save a route as road segments (docs/ARRANQUE.md §5.4)",
		Long: "Save a route as road segments.\n\n" +
			"An existing route with the same name is REPLACED, which is the only way " +
			"to edit one: run the same command again with the segments you want.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre de la ruta: trafico route add <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			segments, _ := cmd.Flags().GetStringArray("segment")
			direction, _ := cmd.Flags().GetString("direction")

			cfg, path, err := loadConfigContext()
			if err != nil {
				return err
			}
			if err := app.AddRoute(cfg, path, args[0], segments, direction); err != nil {
				return err
			}
			routes, err := app.Routes(cfg)
			if err != nil {
				return err
			}
			return writeRoutesResponse(cmd, routes)
		},
	}
	cmd.Flags().StringArray("segment", nil, "tramo de la ruta CARRETERA:DESDE-HASTA, repetible (--segment A-7:102-118 --segment CV-86:0-6)")
	cmd.Flags().String("direction", "both", "dirección de la ruta: positive, negative o both")
	addPrettyFlag(cmd)
	return cmd
}

// newRouteListCommand builds `trafico route list`.
func newRouteListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every saved route with its segments",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigContext()
			if err != nil {
				return err
			}
			routes, err := app.Routes(cfg)
			if err != nil {
				return err
			}
			return writeRoutesResponse(cmd, routes)
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// newRouteShowCommand builds `trafico route show <name>`.
func newRouteShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show one saved route with its segments",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre de la ruta: trafico route show <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigContext()
			if err != nil {
				return err
			}
			routes, err := app.Routes(cfg)
			if err != nil {
				return err
			}
			for _, route := range routes {
				if route.Name == args[0] {
					meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: len(route.Segments)}
					response := routeResponse{
						Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
						Route:    route,
					}
					pretty, _ := cmd.Flags().GetBool("pretty")
					return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
				}
			}
			return errs.NewNotFound(fmt.Sprintf("no existe la ruta %q", args[0]))
		},
	}
}

// newRouteRemoveCommand builds `trafico route remove <name> [--yes]`. On a
// non-interactive invocation there is no prompt at all: without --yes the
// command refuses with a confirmation_required envelope (exit 2) that names
// the flag, and deletes nothing.
func newRouteRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a saved route (requires --yes)",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre de la ruta: trafico route remove <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check the confirmation before anything else: a refused removal
			// must not delete anything.
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				return errs.NewConfirmationRequired(fmt.Sprintf(
					"el borrado de una ruta necesita confirmación: añade --yes para eliminar la ruta %q", args[0]))
			}
			cfg, path, err := loadConfigContext()
			if err != nil {
				return err
			}
			if err := app.RemoveRoute(cfg, path, args[0]); err != nil {
				return err
			}
			routes, err := app.Routes(cfg)
			if err != nil {
				return err
			}
			return writeRoutesResponse(cmd, routes)
		},
	}
	cmd.Flags().Bool("yes", false, "confirma el borrado (sin este flag el comando no elimina nada)")
	addPrettyFlag(cmd)
	return cmd
}

// newRouteCheckCommand builds `trafico route check <name>`: the §5.4
// per-segment report. The application layer (app.RouteCheck) fetches the
// feed exactly once for the whole route and filters per segment.
func newRouteCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check <name>",
		Short: "Report the current incidents on every segment of a saved route, in declaration order",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre de la ruta: trafico route check <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, fetcher, err := loadFeedsContext()
			if err != nil {
				return err
			}
			response, err := app.RouteCheck(cmd.Context(), cfg, fetcher, args[0])
			if err != nil {
				return err
			}
			// Cache-fallback warnings go to stderr (ARRANQUE §5.6, §6.1);
			// stdout stays one pure JSON value (P-01).
			if response.Warning != "" {
				output.Diag(cmd.ErrOrStderr(), "%s", response.Warning)
			}
			pretty, _ := cmd.Flags().GetBool("pretty")
			return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
		},
	}
}

// writeRoutesResponse renders the envelope with the saved-route list. The
// storage commands touch no feed, so meta carries no published_at (nothing
// was fetched and nothing can be stale) and total counts the routes.
func writeRoutesResponse(cmd *cobra.Command, routes []app.RouteInfo) error {
	meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: len(routes)}
	response := routesResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Routes:   routes,
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}
