package app

import (
	"context"
	"fmt"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/geo"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
	"github.com/aam9063/trafico-cli/internal/road"
)

// coverageNoteRegional is the coverage_note literal of docs/ARRANQUE.md §5.3,
// decided by the user and written into docs/feeds.md §4.6: it names the
// regional managers that the DGT feed excludes (SCT in Catalonia, Gobierno
// Vasco), never the provinces, because the feed does publish DGT-managed
// roads inside Catalonia ("no hay datos de Cataluña" would be a lie for the
// AP-7 in Barcelona).
const coverageNoteRegional = "Cobertura de la DGT: no incluye las incidencias gestionadas por la SCT (Cataluña) ni por el Gobierno Vasco"

// coverageProvinces lists the provinces of the regions with a road manager
// other than the State: the four of Catalonia (SCT) and the three of the
// Basque Country (Gobierno Vasco). The coverage note is omitted only when a
// --province filter restricts to provinces outside this list, because only
// then can the absence of incidents not be explained by a missing regional
// manager (docs/feeds.md §4.6, docs/ARRANQUE.md §5.3).
var coverageProvinces = []string{
	"Barcelona",
	"Girona",
	"Tarragona",
	"Lleida",
	"Araba/Álava",
	"Bizkaia",
	"Gipuzkoa",
}

// IncidentCriteria is the query of the incidents command: the common filters
// of ARRANQUE section 5.2 (internal/filter.Criteria) plus the distance
// filter of internal/geo. Incidents applies the radius first and the
// remaining filters second, the composition order both packages document.
type IncidentCriteria struct {
	filter.Criteria
	// Near is a saved place name (config places) that centres the radius.
	Near string
	// Lat and Lon are the coordinates of the radius centre, used together
	// instead of Near. nil means the flag was not set.
	Lat *float64
	Lon *float64
	// RadiusKm is the radius of the distance filter in kilometres; 0 means
	// no geo filter (geo.ResolveTarget owns the exact combination rule).
	RadiusKm float64
}

// IncidentsResponse is the JSON contract of `trafico incidents` and
// `trafico incidents show` (docs/ARRANQUE.md §5.3), validated against
// schemas/incidents-response.schema.json.
type IncidentsResponse struct {
	output.Envelope
	// Query echoes the filters that were actually applied, so an agent can
	// verify what it asked for. Keys the command did not apply are absent.
	Query *incidentsQuery `json:"query,omitempty"`
	// Incidents are the matching incidents, never null in JSON.
	Incidents []model.Incident `json:"incidents"`
	// Warning carries the stderr note of a cache fallback after a network
	// failure (ARRANQUE §5.6). It is deliberately not serialised (P-01:
	// stdout stays one pure JSON value); the CLI layer writes it to stderr.
	Warning string `json:"-"`
}

// pkRange echoes the requested --pk interval with the lowercase keys of
// schemas/incidents-response.schema.json. filter.Range is a domain value
// without JSON tags, so it must not be serialised directly (the contract
// lives in schemas/, not in internal/filter).
type pkRange struct {
	From *float64 `json:"from,omitempty"`
	To   *float64 `json:"to,omitempty"`
}

// incidentsQuery mirrors the documented keys of the query block in
// schemas/incidents-response.schema.json: the filters of §5.2 that were
// applied, in their request form (roads normalised, dates as the instants
// they were parsed into, the geo filter as its centre inputs).
type incidentsQuery struct {
	Road      []string   `json:"road,omitempty"`
	PK        *pkRange   `json:"pk,omitempty"`
	Direction string     `json:"direction,omitempty"`
	Province  []string   `json:"province,omitempty"`
	Type      []string   `json:"type,omitempty"`
	Severity  []string   `json:"severity,omitempty"`
	Status    string     `json:"status,omitempty"`
	From      *time.Time `json:"from,omitempty"`
	To        *time.Time `json:"to,omitempty"`
	Limit     int        `json:"limit,omitempty"`
	Sort      string     `json:"sort,omitempty"`
	Near      string     `json:"near,omitempty"`
	Radius    *float64   `json:"radius,omitempty"`
	// Lat and Lon echo the raw --lat/--lon flags that produced the centre of
	// the radius filter. They stay absent when the centre came from --near
	// or when no geo filter ran (P-04: absent, not null).
	Lat *float64 `json:"lat,omitempty"`
	Lon *float64 `json:"lon,omitempty"`
}

// Incidents answers `trafico incidents`: it fetches the situations feed
// (FEED-01 owns the caching and the fallback), parses it with datex2,
// applies the radius first (geo) and the common filters second (filter),
// and wraps the matches in the §5.3 envelope. Invalid criteria are rejected
// before any request touches the network.
func Incidents(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, criteria IncidentCriteria) (IncidentsResponse, error) {
	if cfg == nil {
		return IncidentsResponse{}, errs.NewInternal("no hay configuración para consultar las incidencias")
	}
	if fetcher == nil {
		return IncidentsResponse{}, errs.NewInternal("no hay descargador de feeds configurado")
	}
	// Reject invalid combinations before spending a request on the feed:
	// Apply over no rows runs exactly its validation. This keeps the
	// ownership of the rules in internal/filter (single source of truth).
	if _, err := filter.Apply(nil, criteria.Criteria); err != nil {
		return IncidentsResponse{}, err
	}
	target, err := geo.ResolveTarget(criteria.Near, criteria.Lat, criteria.Lon, criteria.RadiusKm, cfg.Places)
	if err != nil {
		return IncidentsResponse{}, err
	}
	result, publication, err := fetchSituations(ctx, fetcher)
	if err != nil {
		return IncidentsResponse{}, err
	}
	incidents := buildIncidents(publication, fetcher.Now())

	// Documented composition order (internal/geo, internal/filter docs):
	// radius first, then the common filters, and both unlocated counts land
	// in the same meta.unlocated bucket.
	// Documented composition order (internal/geo, internal/filter docs):
	// radius first, then the common filters, and both unlocated counts land
	// in the same meta.unlocated bucket.
	var geoUnlocated int
	if target != nil {
		incidents, geoUnlocated = geo.FilterRadius(incidents, *target, criteria.RadiusKm)
	}
	matches, err := filter.Apply(incidents, criteria.Criteria)
	if err != nil {
		return IncidentsResponse{}, err
	}
	if matches.Incidents == nil {
		matches.Incidents = []model.Incident{}
	}

	meta := incidentsMeta(result, publication.PublishedAt, fetcher, len(matches.Incidents), geoUnlocated+matches.Unlocated)
	if coverageNoteApplies(criteria.Provinces) {
		meta.CoverageNote = coverageNoteRegional
	}
	return IncidentsResponse{
		Envelope:  output.NewEnvelope(meta, output.NewAttribution(result.FetchedAt)),
		Query:     buildIncidentsQuery(criteria, target),
		Incidents: matches.Incidents,
		Warning:   result.Warning,
	}, nil
}

// IncidentByID returns the incident with the given contract id (the
// "dgt:<record id>" form the model publishes). No filter applies to show
// (ARRANQUE §5.1); an absent id is a not_found error (exit 4).
func IncidentByID(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, id string) (model.Incident, error) {
	detail, err := IncidentDetail(ctx, cfg, fetcher, id)
	if err != nil {
		return model.Incident{}, err
	}
	return detail.Incidents[0], nil
}

// IncidentDetail is the use case behind `trafico incidents show <id>`: the
// §5.3 envelope carrying exactly the requested incident. It reuses the list
// response shape so the output keeps validating against
// schemas/incidents-response.schema.json and the show contract never drifts
// from the list contract.
func IncidentDetail(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, id string) (IncidentsResponse, error) {
	if cfg == nil {
		return IncidentsResponse{}, errs.NewInternal("no hay configuración para consultar las incidencias")
	}
	if fetcher == nil {
		return IncidentsResponse{}, errs.NewInternal("no hay descargador de feeds configurado")
	}
	result, publication, err := fetchSituations(ctx, fetcher)
	if err != nil {
		return IncidentsResponse{}, err
	}
	for _, incident := range buildIncidents(publication, fetcher.Now()) {
		if incident.ID == id {
			// No filter applies to show, so the note always travels: the
			// answer must not read as complete coverage (§5.3).
			meta := incidentsMeta(result, publication.PublishedAt, fetcher, 1, 0)
			meta.CoverageNote = coverageNoteRegional
			return IncidentsResponse{
				Envelope:  output.NewEnvelope(meta, output.NewAttribution(result.FetchedAt)),
				Incidents: []model.Incident{incident},
				Warning:   result.Warning,
			}, nil
		}
	}
	return IncidentsResponse{}, errs.NewNotFound(fmt.Sprintf("no existe la incidencia %q", id))
}

// fetchSituations downloads and parses the situations feed (FEED-01 owns the
// 60 s cache and the fallback after a network failure). An unparsable body
// is a feed change (ARRANQUE §6.3: exit 7, "XML con forma inesperada"),
// never a generic internal error, and it carries the prefilled issue link.
func fetchSituations(ctx context.Context, fetcher *feeds.Fetcher) (*feeds.Result, *datex2.Publication, error) {
	if fetcher == nil {
		return nil, nil, errs.NewInternal("no hay descargador de feeds configurado")
	}
	result, err := fetcher.Fetch(ctx, config.FeedSituations)
	if err != nil {
		return nil, nil, err
	}
	publication, parseErr := datex2.ParseSituations(result.Body())
	closeErr := result.Close()
	switch {
	case parseErr != nil:
		return nil, nil, errs.NewFeedChanged(fmt.Sprintf(
			"el feed %q respondió con un contenido que no se pudo interpretar como XML de incidencias: %v",
			config.FeedSituations, parseErr))
	case closeErr != nil:
		return nil, nil, errs.NewInternal(fmt.Sprintf("no se pudo cerrar la lectura del feed %q: %v", config.FeedSituations, closeErr))
	}
	return result, publication, nil
}

// buildIncidents converts every parsed DATEX II record into a contract
// incident. now is the injected clock of the fetcher: status is computed
// against it, never against wall time, so tests and stale caches stay honest
// (P-08).
func buildIncidents(publication *datex2.Publication, now time.Time) []model.Incident {
	incidents := make([]model.Incident, 0)
	for _, situation := range publication.Situations {
		for _, record := range situation.Records {
			incidents = append(incidents, model.FromRecord(record, nil, now))
		}
	}
	return incidents
}

// incidentsMeta builds the meta block of the incidents envelope with the
// DeriveMeta semantics of the shared commands: published_at from the feed
// itself, age_seconds derived from it (never negative: a publication time in
// the future, a clock skew, reads as age 0), and stale and from_cache only
// when the fetch carried them (P-08).
func incidentsMeta(result *feeds.Result, publishedAt time.Time, fetcher *feeds.Fetcher, total, unlocated int) *output.Meta {
	freshness := result.Freshness(&publishedAt, fetcher.MaxAge, fetcher.Now())
	meta := &output.Meta{
		Feed:        config.FeedSituations,
		FetchedAt:   result.FetchedAt.UTC(),
		PublishedAt: utcTimePtr(freshness.PublishedAt),
		AgeSeconds:  freshness.AgeSeconds,
		Stale:       freshness.Stale,
		FromCache:   result.FromCache,
		Total:       total,
		Unlocated:   unlocated,
	}
	if meta.AgeSeconds != nil && *meta.AgeSeconds < 0 {
		age := int64(0)
		meta.AgeSeconds = &age
	}
	return meta
}

// coverageNoteApplies applies the rule of docs/ARRANQUE.md §5.3: the note
// appears unless a --province filter restricts to provinces where no
// regional manager other than the State exists, in which case the DGT
// coverage is complete for the query and the note would only confuse.
func coverageNoteApplies(provinces []string) bool {
	if len(provinces) == 0 {
		return true
	}
	for _, requested := range provinces {
		for _, managed := range coverageProvinces {
			if model.ProvinceEqual(requested, managed) {
				return true
			}
		}
	}
	return false
}

// buildIncidentsQuery echoes the filters that were actually applied, in the
// documented form: roads normalised, the status default made explicit
// (filter.Apply applies "active" when empty), dates as the parsed instants
// and the geo filter as the inputs that produced its centre. The geo keys
// stay absent when no radius filter ran.
func buildIncidentsQuery(criteria IncidentCriteria, target *model.Coordinates) *incidentsQuery {
	query := &incidentsQuery{
		Direction: criteria.Direction,
		Status:    criteria.Status,
		From:      criteria.From,
		To:        criteria.To,
		Limit:     criteria.Limit,
		Sort:      criteria.Sort,
		Near:      criteria.Near,
	}
	if criteria.PK != nil {
		query.PK = &pkRange{From: criteria.PK.From, To: criteria.PK.To}
	}
	if criteria.Status == "" {
		// The applied default is part of what was asked for (§5.3 echoes it).
		query.Status = "active"
	}
	if len(criteria.Roads) > 0 {
		query.Road = make([]string, len(criteria.Roads))
		for i, r := range criteria.Roads {
			query.Road[i] = road.Normalize(r)
		}
	}
	if len(criteria.Provinces) > 0 {
		query.Province = criteria.Provinces
	}
	if len(criteria.Types) > 0 {
		query.Type = criteria.Types
	}
	if len(criteria.Severities) > 0 {
		query.Severity = criteria.Severities
	}
	if target != nil {
		radius := criteria.RadiusKm
		query.Radius = &radius
		// The centre inputs travel in their request form, so an agent can
		// verify what produced the centre (§5.3). With --near they stay
		// absent: the place name above is the echo of that centre.
		if criteria.Lat != nil {
			query.Lat = criteria.Lat
		}
		if criteria.Lon != nil {
			query.Lon = criteria.Lon
		}
	}
	return query
}
