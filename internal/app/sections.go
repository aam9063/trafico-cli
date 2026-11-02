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

// SectionCriteria is the query of the sections command: the shared filters
// of ARRANQUE §5.2 that make sense for sections. --pk, --type and
// --severity are incident concepts and deliberately absent: the section
// feeds publish no severity or cause, and both feeds are segments, not
// points (docs/feeds.md §7 and §8).
type SectionCriteria struct {
	// Kind selects the dataset: "invive" (the DGT road-section inventory,
	// docs/feeds.md §7) or "animals" (the TEFIVA/SPFRIA black spots for
	// animal collisions, docs/feeds.md §8). It is required: there is no
	// sensible default between two different datasets, so an empty or
	// unknown kind is a usage error.
	Kind string
	// Roads matches any listed road compared with road.Equal, so "cm220",
	// "CM 220" and "CM-220" are the same road.
	Roads []string
	// Direction is "", "positive", "negative" or "both"; anything else is
	// a usage error. "both" (and empty) match everything.
	Direction string
	// Provinces are compared with model.ProvinceEqual against the
	// published province data: INVIVE carries the administrative area name
	// of the primary reference point, TEFIVA carries no province at all
	// (docs/feeds.md §7 and §8), so every animal section is unlocated
	// under a province filter.
	Provinces []string
	// Limit truncates the result; 0 means no limit, negative is a usage
	// error.
	Limit int
}

// SectionsResponse is the JSON contract of `trafico sections`: the §5.3
// envelope with the sections as the payload. Warning carries the stderr
// notes (cache fallback); it is deliberately not serialised (P-01: stdout
// stays one pure JSON value).
type SectionsResponse struct {
	output.Envelope
	// Sections are the matching sections, never null in JSON.
	Sections []model.Section `json:"sections"`
	// Warning is written to stderr by the CLI layer.
	Warning string `json:"-"`
}

// Sections answers `trafico sections --kind <kind>`: it fetches the selected
// section feed (FEED-01 owns the 60 s cache and the fallback), parses it
// with datex2, applies the shared filters and wraps the matches in the §5.3
// envelope. Invalid criteria are rejected before any request touches the
// network.
//
// The section feeds publish no usable publicationTime (docs/feeds.md §7, §8
// and §9), so meta.published_at and meta.age_seconds are null, never
// invented.
func Sections(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, criteria SectionCriteria) (SectionsResponse, error) {
	if cfg == nil {
		return SectionsResponse{}, errs.NewInternal("no hay configuración para consultar los tramos")
	}
	if fetcher == nil {
		return SectionsResponse{}, errs.NewInternal("no hay descargador de feeds configurado")
	}
	// Reject invalid combinations before spending a request on the feed.
	switch criteria.Kind {
	case model.SectionKindInvive, model.SectionKindAnimals:
	default:
		if criteria.Kind == "" {
			return SectionsResponse{}, errs.NewUsage("--kind es obligatorio: usa invive o animals")
		}
		return SectionsResponse{}, errs.NewUsage(fmt.Sprintf(
			"--kind solo acepta invive o animals (recibido %q)", criteria.Kind))
	}
	if criteria.Limit < 0 {
		return SectionsResponse{}, errs.NewUsage("--limit no puede ser negativo")
	}
	switch criteria.Direction {
	case "", "positive", "negative", "both":
	default:
		return SectionsResponse{}, errs.NewUsage(fmt.Sprintf(
			"--direction solo acepta positive, negative o both (recibido %q)", criteria.Direction))
	}

	var feed string
	var result *feeds.Result
	var sections []model.Section
	switch criteria.Kind {
	case model.SectionKindInvive:
		feed = config.FeedSectionsInvive
		res, parsed, err := fetchInvive(ctx, fetcher)
		if err != nil {
			return SectionsResponse{}, err
		}
		result = res
		sections = make([]model.Section, 0, len(parsed))
		for _, raw := range parsed {
			sections = append(sections, model.SectionFromInvive(raw))
		}
	case model.SectionKindAnimals:
		feed = config.FeedSectionsAnimals
		res, parsed, err := fetchTefiva(ctx, fetcher)
		if err != nil {
			return SectionsResponse{}, err
		}
		result = res
		sections = make([]model.Section, 0, len(parsed))
		for _, raw := range parsed {
			sections = append(sections, model.SectionFromTefiva(raw))
		}
	}
	matches, unlocated := filterSections(sections, criteria)

	meta := &output.Meta{
		Feed:      feed,
		FetchedAt: result.FetchedAt.UTC(),
		// docs/feeds.md §9: these feeds publish no usable publicationTime,
		// so published_at and age_seconds stay null — never an invented
		// instant — and an undated feed is not stale.
		PublishedAt: nil,
		AgeSeconds:  nil,
		Stale:       result.Stale,
		FromCache:   result.FromCache,
		Total:       len(matches),
		Unlocated:   unlocated,
	}
	return SectionsResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(result.FetchedAt)),
		Sections: matches,
		Warning:  result.Warning,
	}, nil
}

// fetchInvive downloads and parses the INVIVE section feed. An unparsable
// body is a feed change (ARRANQUE §6.3: exit 7), never a generic internal
// error.
func fetchInvive(ctx context.Context, fetcher *feeds.Fetcher) (*feeds.Result, []datex2.InviveSection, error) {
	result, err := fetcher.Fetch(ctx, config.FeedSectionsInvive)
	if err != nil {
		return nil, nil, err
	}
	parsed, parseErr := datex2.ParseInviveSections(result.Body())
	closeErr := result.Close()
	switch {
	case parseErr != nil:
		return nil, nil, errs.NewFeedChanged(fmt.Sprintf(
			"el feed %q respondió con un contenido que no se pudo interpretar como XML de tramos: %v",
			config.FeedSectionsInvive, parseErr))
	case closeErr != nil:
		return nil, nil, errs.NewInternal(fmt.Sprintf("no se pudo cerrar la lectura del feed %q: %v", config.FeedSectionsInvive, closeErr))
	}
	return result, parsed, nil
}

// fetchTefiva downloads and parses the TEFIVA section feed under the same
// rules as fetchInvive.
func fetchTefiva(ctx context.Context, fetcher *feeds.Fetcher) (*feeds.Result, []datex2.TefivaSection, error) {
	result, err := fetcher.Fetch(ctx, config.FeedSectionsAnimals)
	if err != nil {
		return nil, nil, err
	}
	parsed, parseErr := datex2.ParseTefivaSections(result.Body())
	closeErr := result.Close()
	switch {
	case parseErr != nil:
		return nil, nil, errs.NewFeedChanged(fmt.Sprintf(
			"el feed %q respondió con un contenido que no se pudo interpretar como XML de tramos: %v",
			config.FeedSectionsAnimals, parseErr))
	case closeErr != nil:
		return nil, nil, errs.NewInternal(fmt.Sprintf("no se pudo cerrar la lectura del feed %q: %v", config.FeedSectionsAnimals, closeErr))
	}
	return result, parsed, nil
}

// filterSections applies the criteria that make sense for sections. A
// section with no published province only counts as unlocated when a
// province filter is set: it would have matched but cannot be placed (the
// same meta.unlocated contract as the incidents command). The direction and
// province comparisons are the shared app helpers the radars command uses.
func filterSections(sections []model.Section, criteria SectionCriteria) ([]model.Section, int) {
	matches := make([]model.Section, 0, len(sections))
	unlocated := 0
	for _, section := range sections {
		if len(criteria.Roads) > 0 {
			found := false
			for _, wanted := range criteria.Roads {
				if road.Equal(section.Road, wanted) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if !radarDirectionMatches(section.Direction, criteria.Direction) {
			continue
		}
		if len(criteria.Provinces) > 0 {
			if section.Province == nil {
				unlocated++
				continue
			}
			if !radarProvinceMatches(*section.Province, criteria.Provinces) {
				continue
			}
		}
		matches = append(matches, section)
	}
	if criteria.Limit > 0 && len(matches) > criteria.Limit {
		matches = matches[:criteria.Limit]
	}
	return matches, unlocated
}
