package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newIncidentsCommand builds `trafico incidents` with the filters of
// docs/ARRANQUE.md §5.2 and its `show <id>` subcommand (CMD-01). The filter
// flags are local to the list command on purpose: `incidents show` takes no
// filters, so an unknown flag there must be rejected (exit 2), not ignored.
func newIncidentsCommand() *cobra.Command {
	incidentsCmd := &cobra.Command{
		Use:   "incidents",
		Short: "List current or planned DGT incidents as JSON",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico incidents bogus` is an invalid argument
		// (exit 2) instead of a silent help print (exit 0).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		RunE: runIncidentsList,
	}
	registerIncidentFlags(incidentsCmd.Flags())
	addPrettyFlag(incidentsCmd)
	incidentsCmd.AddCommand(newIncidentsShowCommand())
	return incidentsCmd
}

// registerIncidentFlags registers the common filters of ARRANQUE §5.2.
func registerIncidentFlags(flags *pflag.FlagSet) {
	flags.StringArray("road", nil, "carretera a consultar; repetible (--road A-7 --road AP-7)")
	flags.String("pk", "", "tramo de punto kilométrico from-to, solo con una --road (ejemplo: 100-140)")
	flags.String("direction", "", "dirección de circulación: positive, negative o both")
	flags.StringArray("province", nil, "provincia; repetible (acepta cualquiera de los nombres del par bilingüe)")
	flags.String("near", "", "lugar guardado que centra el filtro de radio (trafico place add)")
	flags.String("radius", "", "radio del filtro de distancia en kilómetros (ejemplo: 15 o 15km)")
	flags.Float64("lat", 0, "latitud que centra el filtro de radio, junto a --lon y --radius")
	flags.Float64("lon", 0, "longitud que centra el filtro de radio, junto a --lat y --radius")
	flags.String("type", "", "tipos normalizados separados por comas (ejemplo: roadworks,accident)")
	flags.String("severity", "", "severidad publicada: low, medium o high")
	flags.String("status", "active", "estado: active, planned o all")
	flags.String("from", "", "inicio de la ventana de fechas (YYYY-MM-DD o RFC3339)")
	flags.String("to", "", "fin de la ventana de fechas; una fecha suelta incluye todo el día")
	flags.Int("limit", 0, "número máximo de incidencias (0 = sin límite)")
	flags.String("sort", "", "orden: severity, pk o start")
}

// runIncidentsList answers `trafico incidents [filtros]`.
func runIncidentsList(cmd *cobra.Command, args []string) error {
	criteria, err := parseIncidentCriteria(cmd)
	if err != nil {
		return err
	}
	cfg, fetcher, err := loadFeedsContext()
	if err != nil {
		return err
	}
	response, err := app.Incidents(cmd.Context(), cfg, fetcher, criteria)
	if err != nil {
		return err
	}
	return writeIncidentsResponse(cmd, response)
}

// newIncidentsShowCommand builds `trafico incidents show <id>`: the detail
// of one incident, with no filters applied.
func newIncidentsShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one incident by its id (dgt:<record id>), with no filters",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el id de una incidencia: trafico incidents show <id>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, fetcher, err := loadFeedsContext()
			if err != nil {
				return err
			}
			response, err := app.IncidentDetail(cmd.Context(), cfg, fetcher, args[0])
			if err != nil {
				return err
			}
			return writeIncidentsResponse(cmd, response)
		},
	}
}

// writeIncidentsResponse renders the §5.3 envelope. Cache-fallback warnings
// go to stderr (ARRANQUE §5.6, §6.1); stdout stays one pure JSON value (P-01).
func writeIncidentsResponse(cmd *cobra.Command, response app.IncidentsResponse) error {
	if response.Warning != "" {
		output.Diag(cmd.ErrOrStderr(), "%s", response.Warning)
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// parseIncidentCriteria translates the §5.2 flags into the application
// criteria. Flag values that cannot be parsed are usage errors (exit 2);
// cross-flag rules (--pk needs one --road, radius combinations, unknown
// enumerations) stay owned by the domain packages (filter, geo).
func parseIncidentCriteria(cmd *cobra.Command) (app.IncidentCriteria, error) {
	flags := cmd.Flags()
	criteria := app.IncidentCriteria{}

	criteria.Roads, _ = flags.GetStringArray("road")

	if value, _ := flags.GetString("pk"); value != "" {
		pkRange, err := parsePKRange(value)
		if err != nil {
			return app.IncidentCriteria{}, err
		}
		criteria.PK = pkRange
	}

	criteria.Direction, _ = flags.GetString("direction")
	criteria.Provinces, _ = flags.GetStringArray("province")
	criteria.Near, _ = flags.GetString("near")

	if value, _ := flags.GetString("radius"); value != "" {
		radiusKm, err := parseRadiusKm(value)
		if err != nil {
			return app.IncidentCriteria{}, err
		}
		criteria.RadiusKm = radiusKm
	}
	if flags.Changed("lat") {
		lat, _ := flags.GetFloat64("lat")
		criteria.Lat = &lat
	}
	if flags.Changed("lon") {
		lon, _ := flags.GetFloat64("lon")
		criteria.Lon = &lon
	}

	if value, _ := flags.GetString("type"); value != "" {
		for _, t := range strings.Split(value, ",") {
			if t = strings.TrimSpace(t); t != "" {
				criteria.Types = append(criteria.Types, t)
			}
		}
	}
	if value, _ := flags.GetString("severity"); value != "" {
		criteria.Severities = []string{value}
	}
	criteria.Status, _ = flags.GetString("status")

	if value, _ := flags.GetString("from"); value != "" {
		from, err := parseWindowDate(value, false)
		if err != nil {
			return app.IncidentCriteria{}, err
		}
		criteria.From = &from
	}
	if value, _ := flags.GetString("to"); value != "" {
		// A bare --to date means the end of that day inclusive: at midnight
		// the filter would silently drop the evening of the last day (the
		// date contract of internal/filter).
		to, err := parseWindowDate(value, true)
		if err != nil {
			return app.IncidentCriteria{}, err
		}
		criteria.To = &to
	}

	criteria.Limit, _ = flags.GetInt("limit")
	criteria.Sort, _ = flags.GetString("sort")
	return criteria, nil
}

// parsePKRange parses --pk as a from-to kilometre range (§5.2: --pk 100-140).
func parsePKRange(value string) (*filter.Range, error) {
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return nil, errs.NewUsage(fmt.Sprintf("--pk debe indicar un rango from-to, por ejemplo 100-140 (recibido %q)", value))
	}
	from, errFrom := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	to, errTo := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if errFrom != nil || errTo != nil {
		return nil, errs.NewUsage(fmt.Sprintf("--pk debe indicar un rango from-to, por ejemplo 100-140 (recibido %q)", value))
	}
	return &filter.Range{From: &from, To: &to}, nil
}

// parseRadiusKm parses --radius as a number of kilometres, with or without
// the explicit unit of §5.2 (--radius 15 or --radius 15km). Any other unit
// is a usage error instead of being silently ignored.
func parseRadiusKm(value string) (float64, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	trimmed = strings.TrimSuffix(trimmed, "km")
	km, err := strconv.ParseFloat(strings.TrimSpace(trimmed), 64)
	if err != nil {
		return 0, errs.NewUsage(fmt.Sprintf("--radius debe ser un número de kilómetros, por ejemplo 15 o 15km (recibido %q)", value))
	}
	// strconv also parses "NaN", "inf" and "infinity"; a non-finite radius
	// must be a usage error before any network call, not a spent request
	// followed by an internal serialisation error (CMD-01 review, blocker 1).
	if math.IsNaN(km) || math.IsInf(km, 0) {
		return 0, errs.NewUsage(fmt.Sprintf("--radius debe ser un número finito de kilómetros, por ejemplo 15 o 15km (recibido %q)", value))
	}
	return km, nil
}

// madridLocation returns Europe/Madrid for the date windows of §5.2. The zone
// database is embedded in the binary (cmd/trafico imports time/tzdata), so this
// succeeds on machines with no zoneinfo; if it somehow does not, the window
// falls back to UTC and keeps working, just anchored to UTC.
func madridLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		return time.UTC
	}
	return loc
}

// parseWindowDate parses --from/--to: an RFC3339 instant is used as-is; a
// bare YYYY-MM-DD is midnight of that day in Europe/Madrid, or — for --to —
// the last nanosecond of that day (23:59:59.999999999), the inclusive
// end-of-day rule documented by internal/filter.
func parseWindowDate(value string, endOfDay bool) (time.Time, error) {
	if instant, err := time.Parse(time.RFC3339, value); err == nil {
		return instant, nil
	}
	loc := madridLocation()
	day, err := time.ParseInLocation("2006-01-02", value, loc)
	if err != nil {
		return time.Time{}, errs.NewUsage(fmt.Sprintf("fecha inválida %q: usa YYYY-MM-DD o RFC3339", value))
	}
	if endOfDay {
		y, m, d := day.In(loc).Date()
		return time.Date(y, m, d, 23, 59, 59, 999999999, loc), nil
	}
	return day, nil
}
