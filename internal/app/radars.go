package app

import (
	"context"
	"fmt"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
	"github.com/aam9063/trafico-cli/internal/road"
)

// RadarCriteria is the query of the radars command: the shared filters of
// ARRANQUE §5.2 that make sense for radars. --pk, --type and --severity
// are incident concepts and deliberately absent: the radar feed publishes
// no severity or cause, and its kilometre points arrive in metres without
// a point query to filter on (docs/feeds.md §6).
type RadarCriteria struct {
	// Roads matches any listed road compared with road.Equal, so "z40",
	// "Z 40" and "Z-40" are the same road.
	Roads []string
	// Direction is "", "positive", "negative" or "both"; anything else is
	// a usage error. "both" (and empty) match everything.
	Direction string
	// Provinces are compared with model.ProvinceEqual against the province
	// name resolved from the INE table (model.ProvinceFromINE), so
	// "zaragoza" matches the radar whose provinceINEIdentifier is 50 —
	// the whole point of the table (docs/feeds.md §6).
	Provinces []string
	// Limit truncates the result; 0 means no limit, negative is a usage
	// error.
	Limit int
}

// RadarsResponse is the JSON contract of `trafico radars`: the §5.3
// envelope with the radars as the payload. Warning carries the stderr notes
// (cache fallback); it is deliberately not serialised (P-01: stdout stays
// one pure JSON value).
type RadarsResponse struct {
	output.Envelope
	// Radars are the matching radars, never null in JSON.
	Radars []model.Radar `json:"radars"`
	// Warning is written to stderr by the CLI layer.
	Warning string `json:"-"`
}

// Radars answers `trafico radars`: it fetches the radar feed (FEED-01 owns
// the 60 s cache and the fallback), parses it with datex2, applies the
// shared filters and wraps the matches in the §5.3 envelope. Invalid
// criteria are rejected before any request touches the network.
//
// The feed publishes no usable publicationTime (docs/feeds.md §6 and §9),
// so meta.published_at and meta.age_seconds are null, never invented.
func Radars(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, criteria RadarCriteria) (RadarsResponse, error) {
	if cfg == nil {
		return RadarsResponse{}, errs.NewInternal("no hay configuración para consultar los radares")
	}
	if fetcher == nil {
		return RadarsResponse{}, errs.NewInternal("no hay descargador de feeds configurado")
	}
	// Reject invalid combinations before spending a request on the feed.
	if criteria.Limit < 0 {
		return RadarsResponse{}, errs.NewUsage("--limit no puede ser negativo")
	}
	switch criteria.Direction {
	case "", "positive", "negative", "both":
	default:
		return RadarsResponse{}, errs.NewUsage(fmt.Sprintf(
			"--direction solo acepta positive, negative o both (recibido %q)", criteria.Direction))
	}

	result, radars, err := fetchRadars(ctx, fetcher)
	if err != nil {
		return RadarsResponse{}, err
	}
	matches, unlocated := filterRadars(radars, criteria)

	meta := &output.Meta{
		Feed:      config.FeedRadars,
		FetchedAt: result.FetchedAt.UTC(),
		// docs/feeds.md §9: this feed publishes no usable publicationTime,
		// so published_at and age_seconds stay null — never an invented
		// instant — and an undated feed is not stale.
		PublishedAt: nil,
		AgeSeconds:  nil,
		Stale:       result.Stale,
		FromCache:   result.FromCache,
		Total:       len(matches),
		Unlocated:   unlocated,
	}
	return RadarsResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(result.FetchedAt)),
		Radars:   matches,
		Warning:  result.Warning,
	}, nil
}

// fetchRadars downloads and parses the radar feed. An unparsable body is a
// feed change (ARRANQUE §6.3: exit 7), never a generic internal error.
func fetchRadars(ctx context.Context, fetcher *feeds.Fetcher) (*feeds.Result, []model.Radar, error) {
	result, err := fetcher.Fetch(ctx, config.FeedRadars)
	if err != nil {
		return nil, nil, err
	}
	parsed, parseErr := datex2.ParseRadars(result.Body())
	closeErr := result.Close()
	switch {
	case parseErr != nil:
		return nil, nil, errs.NewFeedChanged(fmt.Sprintf(
			"el feed %q respondió con un contenido que no se pudo interpretar como XML de radares: %v",
			config.FeedRadars, parseErr))
	case closeErr != nil:
		return nil, nil, errs.NewInternal(fmt.Sprintf("no se pudo cerrar la lectura del feed %q: %v", config.FeedRadars, closeErr))
	}
	radars := make([]model.Radar, 0, len(parsed))
	for _, radar := range parsed {
		radars = append(radars, model.RadarFromParsed(radar))
	}
	return result, radars, nil
}

// filterRadars applies the criteria that make sense for radars. A radar
// whose INE code resolves to no province only counts as unlocated when a
// province filter is set: it would have matched but cannot be placed (the
// same meta.unlocated contract as the incidents command).
func filterRadars(radars []model.Radar, criteria RadarCriteria) ([]model.Radar, int) {
	matches := make([]model.Radar, 0, len(radars))
	unlocated := 0
	for _, radar := range radars {
		if !radarRoadMatches(radar, criteria.Roads) {
			continue
		}
		if !radarDirectionMatches(radar.Direction, criteria.Direction) {
			continue
		}
		if len(criteria.Provinces) > 0 {
			if radar.Province == nil {
				unlocated++
				continue
			}
			if !radarProvinceMatches(*radar.Province, criteria.Provinces) {
				continue
			}
		}
		matches = append(matches, radar)
	}
	if criteria.Limit > 0 && len(matches) > criteria.Limit {
		matches = matches[:criteria.Limit]
	}
	return matches, unlocated
}

func radarRoadMatches(radar model.Radar, roads []string) bool {
	if len(roads) == 0 {
		return true
	}
	for _, wanted := range roads {
		if road.Equal(radar.Road, wanted) {
			return true
		}
	}
	return false
}

// radarDirectionMatches mirrors the direction semantics of internal/filter
// (docs/feeds.md §4.4): "both" means no filter, a radar travelling "both"
// directions matches a one-way filter, and a radar with no published
// direction only matches when no direction filter is set.
func radarDirectionMatches(direction *string, filter string) bool {
	switch filter {
	case "", "both":
		return true
	case "positive", "negative":
		if direction == nil {
			return false
		}
		return *direction == filter || *direction == "both"
	default:
		return true
	}
}

func radarProvinceMatches(province string, provinces []string) bool {
	for _, wanted := range provinces {
		if model.ProvinceEqual(province, wanted) {
			return true
		}
	}
	return false
}
