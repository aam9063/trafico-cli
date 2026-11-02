package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
	"github.com/aam9063/trafico-cli/internal/road"
)

// RouteSegmentView is one parsed route segment as the route commands echo
// it: the normalised road name and the kilometre-point interval, both
// bounds present (config.ParseRouteSegment guarantees FROM <= TO).
type RouteSegmentView struct {
	Road string  `json:"road"`
	From float64 `json:"from"`
	To   float64 `json:"to"`
}

// RouteInfo is one saved route of the JSON contract of `trafico route list`
// and `trafico route show` (ROUTE-01). Direction is omitted when stored
// empty, which means "both" (P-04: absent, not invented).
type RouteInfo struct {
	Name      string             `json:"name"`
	Direction string             `json:"direction,omitempty"`
	Segments  []RouteSegmentView `json:"segments"`
}

// Routes lists every saved route, in name order, with its segments parsed
// in declaration order.
func Routes(cfg *config.Config) ([]RouteInfo, error) {
	if cfg == nil {
		return nil, errs.NewInternal("no hay configuración para listar las rutas")
	}
	names := make([]string, 0, len(cfg.Routes))
	for name := range cfg.Routes {
		names = append(names, name)
	}
	sort.Strings(names)

	routes := make([]RouteInfo, 0, len(names))
	for _, name := range names {
		segments, err := parseStoredSegments(name, cfg.Routes[name].Segments)
		if err != nil {
			return nil, err
		}
		routes = append(routes, RouteInfo{
			Name:      name,
			Direction: cfg.Routes[name].Direction,
			Segments:  segments,
		})
	}
	return routes, nil
}

// AddRoute saves a route and persists the config. The write goes through
// config.Save (atomic, 0600) — no other code writes the config file — and
// every validation failure is a usage error (exit 2) before anything is
// written, so a rejected add leaves the file byte-identical.
func AddRoute(cfg *config.Config, configPath, name string, segments []string, direction string) error {
	if cfg == nil {
		return errs.NewInternal("no hay configuración para guardar la ruta")
	}
	if err := config.ValidateRouteName(name); err != nil {
		return errs.NewUsage(err.Error())
	}
	if len(segments) == 0 {
		return errs.NewUsage("indica al menos un tramo con --segment CARRETERA:DESDE-HASTA, por ejemplo --segment A-7:102-118")
	}
	for _, segment := range segments {
		if _, err := config.ParseRouteSegment(segment); err != nil {
			return errs.NewUsage(err.Error())
		}
	}
	switch direction {
	case "", "positive", "negative", "both":
	default:
		return errs.NewUsage("--direction debe ser positive, negative o both")
	}

	stored := make([]string, 0, len(segments))
	for _, segment := range segments {
		stored = append(stored, strings.TrimSpace(segment))
	}
	cfg.Routes[name] = config.Route{Direction: direction, Segments: stored}
	if err := config.Save(configPath, cfg); err != nil {
		// Keep the in-memory config consistent with the disk after a failed
		// atomic write.
		delete(cfg.Routes, name)
		return err
	}
	return nil
}

// RemoveRoute deletes a saved route and persists the config through
// config.Save. An unknown route is a not_found error (exit 4).
func RemoveRoute(cfg *config.Config, configPath, name string) error {
	if cfg == nil {
		return errs.NewInternal("no hay configuración para eliminar la ruta")
	}
	stored, ok := cfg.Routes[name]
	if !ok {
		return errs.NewNotFound(fmt.Sprintf("no existe la ruta %q", name))
	}
	delete(cfg.Routes, name)
	if err := config.Save(configPath, cfg); err != nil {
		cfg.Routes[name] = stored
		return err
	}
	return nil
}

// routeCheckPK echoes the kilometre interval of one segment (docs/
// ARRANQUE.md §5.4: pk { from, to }).
type routeCheckPK struct {
	From float64 `json:"from"`
	To   float64 `json:"to"`
}

// RouteCheckSegment is one entry of the `trafico route check` output
// (docs/ARRANQUE.md §5.4): the segment as declared, with the incidents that
// match its road and kilometre range. The incidents reuse the model of the
// incidents contract, so every segment validates like an incidents answer.
type RouteCheckSegment struct {
	Road      string           `json:"road"`
	PK        routeCheckPK     `json:"pk"`
	Incidents []model.Incident `json:"incidents"`
	// Signs and Radars are null unless the caller asked for those feeds
	// (RouteCheckOptions). The keys stay in the contract either way, so a
	// consumer sees the same shape whether or not the extra feeds were
	// loaded — the same reason speed_limit exists as null on a radar.
	Signs  []model.Sign  `json:"signs"`
	Radars []model.Radar `json:"radars"`
}

// RouteCheckOptions selects the extra feeds a route check may load. The
// situation feed is always fetched; the others are opt-in because each is
// another full download, and the default check stays one request (the
// ROUTE-01 constraint). Flipping both on costs three feeds for the route.
type RouteCheckOptions struct {
	Signs  bool
	Radars bool
}

// RouteCheckSummary is the §5.4 summary block: how many incidents the whole
// route carries, how many are high severity and how many are closures.
type RouteCheckSummary struct {
	Incidents    int `json:"incidents"`
	HighSeverity int `json:"high_severity"`
	Closures     int `json:"closures"`
	Signs        int `json:"signs"`
	Radars       int `json:"radars"`
}

// RouteCheckResponse is the JSON contract of `trafico route check <name>`
// (docs/ARRANQUE.md §5.4). Segments appear in the order the route declares
// them, never in feed or map order.
type RouteCheckResponse struct {
	output.Envelope
	Route    string              `json:"route"`
	Segments []RouteCheckSegment `json:"segments"`
	Summary  RouteCheckSummary   `json:"summary"`
	// Warning carries the stderr note of a cache fallback after a network
	// failure (ARRANQUE §5.6). It is deliberately not serialised (P-01);
	// the CLI layer writes it to stderr.
	Warning string `json:"-"`
}

// RouteCheck answers `trafico route check <name>`: one entry per declared
// segment, in declaration order, plus the §5.4 summary. It makes exactly
// ONE fetch of the situations feed for the whole route: the feed is
// downloaded and parsed once below and the per-segment matching runs over
// the same in-memory incidents — fetching inside the segment loop would
// cost one full ~7 MB download per segment (P-06, ROUTE-01 constraint).
//
// The route's stored direction is metadata for the future watch command
// (§5.5): the check reports everything the feed publishes on each segment,
// so a jam in the opposite carriageway is still visible to the agent.
func RouteCheck(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, name string) (RouteCheckResponse, error) {
	return RouteCheckWith(ctx, cfg, fetcher, name, RouteCheckOptions{})
}

// RouteCheckWith is RouteCheck with the extra feeds selected by opts. Each
// requested feed is fetched once for the whole route, never per segment.
func RouteCheckWith(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, name string, opts RouteCheckOptions) (RouteCheckResponse, error) {
	if cfg == nil {
		return RouteCheckResponse{}, errs.NewInternal("no hay configuración para consultar la ruta")
	}
	if fetcher == nil {
		return RouteCheckResponse{}, errs.NewInternal("no hay descargador de feeds configurado")
	}
	stored, ok := cfg.Routes[name]
	if !ok {
		return RouteCheckResponse{}, errs.NewNotFound(fmt.Sprintf("no existe la ruta %q", name))
	}
	// Parse every declared segment before touching the network: the config
	// was validated on load, but the failure must stay a usage error
	// (exit 2), never a spent request.
	segments, err := parseStoredSegments(name, stored.Segments)
	if err != nil {
		return RouteCheckResponse{}, err
	}

	// The single fetch of the whole route check (see the function comment).
	result, publication, err := fetchSituations(ctx, fetcher)
	if err != nil {
		return RouteCheckResponse{}, err
	}
	incidents := buildIncidents(publication, fetcher.Now())

	// The optional feeds are fetched once here, outside the segment loop,
	// for the same reason the situation feed is: a per-segment fetch would
	// mean one full download per declared segment.
	var allSigns []model.Sign
	if opts.Signs {
		signsResponse, err := Signs(ctx, cfg, fetcher, SignCriteria{})
		if err != nil {
			return RouteCheckResponse{}, err
		}
		allSigns = signsResponse.Signs
	}
	var allRadars []model.Radar
	if opts.Radars {
		radarsResponse, err := Radars(ctx, cfg, fetcher, RadarCriteria{})
		if err != nil {
			return RouteCheckResponse{}, err
		}
		allRadars = radarsResponse.Radars
	}

	response := RouteCheckResponse{
		Route:    name,
		Segments: make([]RouteCheckSegment, 0, len(segments)),
	}
	for _, segment := range segments {
		// filter.Apply owns the road and PK-overlap semantics (single
		// source of truth); the defaults match `trafico incidents`: status
		// active, no direction filter, no sort.
		matches, err := filter.Apply(incidents, filter.Criteria{
			Roads: []string{segment.Road},
			PK:    &filter.Range{From: &segment.From, To: &segment.To},
		})
		if err != nil {
			// Unreachable: ParseRouteSegment validated the bounds, so the
			// criteria cannot be invalid. Surface it rather than hide it.
			return RouteCheckResponse{}, err
		}
		if matches.Incidents == nil {
			matches.Incidents = []model.Incident{}
		}
		for _, incident := range matches.Incidents {
			response.Summary.Incidents++
			if strings.EqualFold(incident.Severity, "high") {
				response.Summary.HighSeverity++
			}
			if strings.EqualFold(incident.Type, "closure") {
				response.Summary.Closures++
			}
		}
		entry := RouteCheckSegment{
			Road:      segment.Road,
			PK:        routeCheckPK{From: segment.From, To: segment.To},
			Incidents: matches.Incidents,
		}
		if opts.Radars {
			entry.Radars = make([]model.Radar, 0)
			for _, radar := range allRadars {
				if radarOnSegment(radar, segment) {
					entry.Radars = append(entry.Radars, radar)
				}
			}
			response.Summary.Radars += len(entry.Radars)
		}
		if opts.Signs {
			entry.Signs = make([]model.Sign, 0)
			for _, sign := range allSigns {
				if signOnSegment(sign, segment) {
					entry.Signs = append(entry.Signs, sign)
				}
			}
			response.Summary.Signs += len(entry.Signs)
		}
		response.Segments = append(response.Segments, entry)
	}

	// meta.total counts the incidents across segments, the same number the
	// summary reports; a route with overlapping segments may therefore
	// count one incident twice, which is honest: it appears in both
	// segments. Unlocated stays 0: no province filter applies to a route.
	meta := incidentsMeta(result, publication.PublishedAt, fetcher, response.Summary.Incidents, 0)
	// A route filter is not a province filter, so the coverage note always
	// travels, like `incidents show` (§5.3).
	meta.CoverageNote = coverageNoteRegional
	response.Envelope = output.NewEnvelope(meta, output.NewAttribution(result.FetchedAt))
	response.Warning = result.Warning
	return response, nil
}

// parseStoredSegments parses the stored "ROAD:FROM-TO" segments of one
// route in declaration order, normalising the road names. A stored segment
// that fails to parse is a usage error (exit 2): the config validation on
// load makes it unreachable through the CLI, but it must never surface as
// an internal error.
// radarOnSegment reports whether a radar covers any part of the segment. A
// segment radar publishes a range whose bounds may arrive unordered
// (docs/feeds.md §6: 2650 m ini, 1400 m fin), so the interval is sorted
// before the overlap test — otherwise a reversed radar would never match.
func radarOnSegment(radar model.Radar, segment RouteSegmentView) bool {
	if radar.PK == nil || radar.PK.From == nil || radar.PK.To == nil {
		return false
	}
	if !road.Equal(radar.Road, segment.Road) {
		return false
	}
	low, high := *radar.PK.From, *radar.PK.To
	if low > high {
		low, high = high, low
	}
	return low <= segment.To && segment.From <= high
}

// signOnSegment reports whether a panel sits at a point inside the segment.
// A panel publishes a single kilometre point, not a range.
func signOnSegment(sign model.Sign, segment RouteSegmentView) bool {
	if sign.PK == nil {
		return false
	}
	if !road.Equal(sign.Road, segment.Road) {
		return false
	}
	return *sign.PK >= segment.From && *sign.PK <= segment.To
}

func parseStoredSegments(route string, stored []string) ([]RouteSegmentView, error) {
	segments := make([]RouteSegmentView, 0, len(stored))
	for _, segment := range stored {
		parsed, err := config.ParseRouteSegment(segment)
		if err != nil {
			return nil, errs.NewUsage(fmt.Sprintf("la ruta %q tiene un tramo no válido: %v", route, err))
		}
		segments = append(segments, RouteSegmentView{Road: parsed.Road, From: parsed.From, To: parsed.To})
	}
	return segments, nil
}
