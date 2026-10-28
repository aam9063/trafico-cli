package cli

import (
	"fmt"
	"time"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
)

// placesResponse is the JSON contract of `trafico place add/list/remove`
// (ROUTE-01): the shared envelope with the full saved-place list, so a
// mutation shows its result and list stays a plain query. The saved
// coordinates are personal data (P-07): they never leave the machine, they
// are only echoed back to the local caller.
type placesResponse struct {
	output.Envelope
	Places []app.Place `json:"places"`
}

// newPlaceCommand builds `trafico place` with its add, list and remove
// subcommands (docs/ARRANQUE.md §5.1). The commands only read and write the
// local config: they never build a feed fetcher and never touch the network.
func newPlaceCommand() *cobra.Command {
	placeCmd := &cobra.Command{
		Use:   "place",
		Short: "Manage saved places for the --near radius filter (stored locally, never sent anywhere)",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico place bogus` is an invalid argument (exit 2,
		// ARRANQUE §6.3) instead of a silent help print (exit 0).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return errs.NewUsage("indica un subcomando: add, list o remove")
		},
	}
	placeCmd.AddCommand(newPlaceAddCommand(), newPlaceListCommand(), newPlaceRemoveCommand())
	return placeCmd
}

// newPlaceAddCommand builds `trafico place add <name> --lat <f> --lon <f>`.
func newPlaceAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Save a place as coordinates (used by --near)",
		Long: "Save a place as coordinates.\n\n" +
			"An existing place with the same name is REPLACED, which is the only way " +
			"to move one.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre del lugar: trafico place add <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			if !flags.Changed("lat") || !flags.Changed("lon") {
				return errs.NewUsage("indica las coordenadas del lugar con --lat y --lon, por ejemplo --lat 38.34 --lon -0.49")
			}
			lat, _ := flags.GetFloat64("lat")
			lon, _ := flags.GetFloat64("lon")

			cfg, path, err := loadConfigContext()
			if err != nil {
				return err
			}
			if err := app.AddPlace(cfg, path, args[0], lat, lon); err != nil {
				return err
			}
			places, err := app.Places(cfg)
			if err != nil {
				return err
			}
			return writePlacesResponse(cmd, places)
		},
	}
	cmd.Flags().Float64("lat", 0, "latitud del lugar (obligatoria)")
	cmd.Flags().Float64("lon", 0, "longitud del lugar (obligatoria)")
	addPrettyFlag(cmd)
	return cmd
}

// newPlaceListCommand builds `trafico place list`.
func newPlaceListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every saved place with its coordinates",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigContext()
			if err != nil {
				return err
			}
			places, err := app.Places(cfg)
			if err != nil {
				return err
			}
			return writePlacesResponse(cmd, places)
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// newPlaceRemoveCommand builds `trafico place remove <name> [--yes]`. On a
// non-interactive invocation there is no prompt at all: without --yes the
// command refuses with a confirmation_required envelope (exit 2) that names
// the flag, and deletes nothing.
func newPlaceRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a saved place (requires --yes)",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre del lugar: trafico place remove <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check the confirmation before anything else: a refused removal
			// must not even load-mutate-save, just refuse.
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				return errs.NewConfirmationRequired(fmt.Sprintf(
					"el borrado de un lugar necesita confirmación: añade --yes para eliminar el lugar %q", args[0]))
			}
			cfg, path, err := loadConfigContext()
			if err != nil {
				return err
			}
			if err := app.RemovePlace(cfg, path, args[0]); err != nil {
				return err
			}
			places, err := app.Places(cfg)
			if err != nil {
				return err
			}
			return writePlacesResponse(cmd, places)
		},
	}
	cmd.Flags().Bool("yes", false, "confirma el borrado (sin este flag el comando no elimina nada)")
	addPrettyFlag(cmd)
	return cmd
}

// writePlacesResponse renders the envelope with the saved-place list. The
// storage commands touch no feed, so meta carries no published_at (nothing
// was fetched and nothing can be stale) and total counts the places.
func writePlacesResponse(cmd *cobra.Command, places []app.Place) error {
	meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: len(places)}
	response := placesResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Places:   places,
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// loadConfigContext resolves the config path and loads the configuration,
// without building a feed fetcher: the place and route storage commands
// never touch the network (P-07).
func loadConfigContext() (*config.Config, string, error) {
	paths, err := config.ResolvePaths(config.OSEnv)
	if err != nil {
		return nil, "", errs.NewInternal(err.Error())
	}
	cfg, err := config.Load(paths.ConfigPath)
	if err != nil {
		return nil, "", errs.NewInternal(err.Error())
	}
	return cfg, paths.ConfigPath, nil
}
