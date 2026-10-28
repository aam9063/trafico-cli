// Package filter applies the common query filters of ARRANQUE section 5.2
// (road, PK range, direction, province, type, severity, status, date window,
// limit, sort) over in-memory incidents. It is pure logic: no IO, no HTTP,
// no printing, no cobra. The CLI layer (internal/cli, internal/app) owns the
// flag parsing and the JSON envelope, including meta.unlocated, which it
// copies from Result.Unlocated.
//
// Composition with the distance layer: radius filtering lives in
// internal/geo (FilterRadius, ResolveTarget). The caller applies the radius
// first and adds the geo unlocated count (incidents without coordinates) to
// filter.Result.Unlocated, so both kinds of dropped rows land in the same
// meta.unlocated bucket required by section 5.2.
//
// Rules this package fixes where section 5.2 left room:
//
//   - Status: Apply applies the "active" default itself when Criteria.Status
//     is empty; the caller does not need to normalise it.
//   - PK: a range matches on overlap, never containment: the incident
//     interval [pk.from, pk.to] and the requested range overlap when
//     max(lower bounds) <= min(upper bounds). An incident with a nil PK
//     never matches, and neither does an incident PK with both bounds nil:
//     the feed said nothing about its extent, so it claims no knowledge.
//     A missing bound is open-ended; a point location (one bound) is a
//     degenerate interval. A requested Range with both bounds nil imposes
//     no PK constraint, and From > To is a usage error.
//   - Direction: a "both" filter means no filter. An incident with a null
//     direction only matches when no direction filter is set; an incident
//     with "both" blocks both directions of travel (docs/feeds.md section
//     4.4) and therefore matches a positive and a negative filter alike.
//   - Unlocated: a row counts as unlocated only after it has passed the
//     positive-match filters (road, PK, direction, type, severity, status,
//     window) and is then rejected for carrying no province. Composition
//     contract with the distance layer: the caller applies the radius
//     first (internal/geo), then Apply, and adds the geo count to
//     Result.Unlocated, so meta.unlocated means "rows that would have
//     matched but could not be placed".
//   - Sort ties: the sort is stable, so incidents with an equal key keep
//     their input order. Apply is therefore deterministic.
//   - Unknown values for status, direction or sort are usage errors
//     (errs.NewUsage, exit code 2), like --pk without exactly one --road or
//     a negative --limit.
//   - Dates: From and To are instants, while section 5.2 shows dates. The
//     caller owns the date-to-instant parsing and must pass an inclusive
//     end-of-day for To; a To at midnight of the last day silently drops
//     everything that happens after midnight that day. The overlap
//     semantics themselves are unchanged.
package filter

import (
	"sort"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/road"
)

// Range is a kilometre-point interval as requested by --pk 100-140. A nil
// bound is open-ended; both bounds nil means no PK constraint.
type Range struct {
	From *float64
	To   *float64
}

// Criteria mirrors the common filters of ARRANQUE section 5.2. The zero
// value is valid and means "no filter, default status": Apply treats an
// empty Status as "active", an empty Sort as "input order" and Limit 0 as
// "no limit".
type Criteria struct {
	// Roads matches any listed road compared with road.Equal, so "a7",
	// "A 7" and "A-7" are the same road.
	Roads []string
	// PK is only valid with exactly one road (ARRANQUE section 6.3:
	// otherwise a usage error, exit code 2).
	PK *Range
	// Direction is "", "positive", "negative" or "both"; anything else is a
	// usage error. "both" matches everything.
	Direction string
	// Provinces are compared with model.ProvinceEqual (case, accents and
	// bilingual pairs like "Alacant/Alicante" fold). An incident with no
	// province never matches a province filter and is counted in
	// Result.Unlocated.
	Provinces []string
	// Types and Severities are normalised lowercase; empty means no filter.
	Types      []string
	Severities []string
	// Status is "", "active" (default), "planned" or "all"; anything else
	// is a usage error. Apply applies the "active" default when empty.
	Status string
	// From and To bound the requested date window for planned incidents
	// (and any incident): the incident validity window must overlap it. A
	// missing incident end is open-ended; a missing window bound too.
	From *time.Time
	To   *time.Time
	// Limit truncates the result after sorting; 0 means no limit.
	Limit int
	// Sort is "", "severity" (high first, unknown last), "pk" (ascending,
	// null last) or "start" (earliest first); anything else is a usage
	// error.
	Sort string
}

// Result carries the matching incidents and Unlocated, the count of
// incidents dropped because a province filter was set and the incident
// carries no province (ARRANQUE section 5.2, meta.unlocated). The caller
// adds incidents dropped by a radius filter without coordinates to the same
// count.
type Result struct {
	Incidents []model.Incident
	Unlocated int
}

// severityRank orders severity for the "severity" sort: high first,
// unknown (the DGT publishes no severity) last. severityRankOf also ranks
// an empty or unmapped severity last, so the "unknown last" guarantee holds
// for any input the model does not produce.
var severityRanks = map[string]int{"high": 0, "medium": 1, "low": 2, "unknown": 3}

func severityRank(s string) int {
	if r, ok := severityRanks[s]; ok {
		return r
	}
	return 3
}

// valid values for the enumerated filters; anything else is a usage error.
var (
	validDirections = map[string]bool{"": true, "positive": true, "negative": true, "both": true}
	validStatuses   = map[string]bool{"": true, "active": true, "planned": true, "all": true}
	validSorts      = map[string]bool{"": true, "severity": true, "pk": true, "start": true}
)

// Apply filters incidents and returns the matches plus the unlocated count.
// It never mutates its input and is deterministic: the same input always
// yields the same order (stable sorts, filters applied in input order).
func Apply(incidents []model.Incident, c Criteria) (Result, error) {
	if err := validate(c); err != nil {
		return Result{}, err
	}

	status := c.Status
	if status == "" {
		status = "active"
	}

	roads := make([]string, len(c.Roads))
	for k, r := range c.Roads {
		roads[k] = road.Normalize(r)
	}
	provinces := setLower(c.Provinces)
	types := setLower(c.Types)
	severities := setLower(c.Severities)
	byRoad := len(roads) == 1 && c.PK != nil

	var (
		matches   []model.Incident
		unlocated int
	)
	for _, inc := range incidents {
		if len(roads) > 0 && !matchesRoad(inc.Road, roads) {
			continue
		}
		if byRoad && !pkOverlaps(inc.PK, c.PK) {
			continue
		}
		if !directionMatches(inc.Direction, c.Direction) {
			continue
		}
		if len(types) > 0 && !types[strings.ToLower(inc.Type)] {
			continue
		}
		if len(severities) > 0 && !severities[strings.ToLower(inc.Severity)] {
			continue
		}
		if status != "all" && inc.Status != status {
			continue
		}
		if c.From != nil || c.To != nil {
			if !windowOverlaps(inc.StartTime, inc.EndTime, c.From, c.To) {
				continue
			}
		}
		if len(provinces) > 0 {
			if inc.Location.Province == "" {
				// Section 5.2: a record with no province is not included
				// and is counted in meta.unlocated, so a user filtering by
				// province knows some rows could not be placed. The check
				// runs last: a row only counts as unlocated when it would
				// otherwise have matched.
				unlocated++
				continue
			}
			if !matchesProvince(inc.Location.Province, provinces) {
				continue
			}
		}
		// Copy the loop variable into a fresh backing array so the result
		// never aliases the caller's slice.
		matches = append(matches, inc)
	}

	sortMatches(matches, c.Sort)
	if c.Limit > 0 && len(matches) > c.Limit {
		matches = matches[:c.Limit]
	}
	return Result{Incidents: matches, Unlocated: unlocated}, nil
}

// validate rejects the invalid filter combinations of ARRANQUE section 6.3
// as usage errors (exit code 2): --pk without exactly one --road, inverted
// ranges, a negative --limit and unknown enumeration values.
func validate(c Criteria) error {
	if c.PK != nil && len(c.Roads) != 1 {
		return errs.NewUsage("--pk solo es válido con exactamente una --road")
	}
	if c.PK != nil && c.PK.From != nil && c.PK.To != nil && *c.PK.From > *c.PK.To {
		return errs.NewUsage("el rango --pk está invertido: el inicio supera el fin")
	}
	if !validDirections[c.Direction] {
		return errs.NewUsage("--direction debe ser positive, negative o both")
	}
	if !validStatuses[c.Status] {
		return errs.NewUsage("--status debe ser active, planned o all")
	}
	if !validSorts[c.Sort] {
		return errs.NewUsage("--sort debe ser severity, pk o start")
	}
	if c.From != nil && c.To != nil && c.From.After(*c.To) {
		return errs.NewUsage("la ventana de fechas está invertida: --from supera --to")
	}
	if c.Limit < 0 {
		return errs.NewUsage("--limit no puede ser negativo")
	}
	return nil
}

func matchesRoad(incidentRoad string, roads []string) bool {
	for _, want := range roads {
		if road.Equal(incidentRoad, want) {
			return true
		}
	}
	return false
}

func matchesProvince(incidentProvince string, provinces map[string]bool) bool {
	for half := range provinces {
		if model.ProvinceEqual(incidentProvince, half) {
			return true
		}
	}
	return false
}

// directionMatches: a "both" filter means no filter. An incident with a
// null direction only matches when no direction filter is set; an incident
// with "both" blocks both directions of travel (docs/feeds.md section 4.4)
// and therefore matches a positive and a negative filter alike.
func directionMatches(incidentDirection *string, want string) bool {
	if want == "" || want == "both" {
		return true
	}
	if incidentDirection == nil {
		return false
	}
	return *incidentDirection == want || *incidentDirection == "both"
}

// pkOverlaps reports whether the incident kilometre-point interval overlaps
// the requested range. Overlap, not containment: an incident spanning 95-105
// matches --pk 100-140. A nil incident PK never matches, and neither does a
// PK with both bounds nil (the feed gave no bounds); a missing bound is
// open-ended; a point location (one null bound) is a degenerate interval.
func pkOverlaps(inc *model.PK, r *Range) bool {
	if inc == nil || r == nil {
		return false
	}
	if inc.From == nil && inc.To == nil {
		return false
	}
	lo, hi := bounds(inc.From, inc.To)
	rlo, rhi := bounds(r.From, r.To)
	if rlo != nil && hi != nil && *rlo > *hi {
		return false
	}
	if lo != nil && rhi != nil && *lo > *rhi {
		return false
	}
	return true
}

// bounds returns the [lo, hi] of a possibly open-ended interval with
// optional bounds, or the degenerate point when only one bound is set.
func bounds(from, to *float64) (lo, hi *float64) {
	switch {
	case from != nil && to != nil:
		if *from <= *to {
			return from, to
		}
		return to, from
	case from != nil:
		return from, from
	case to != nil:
		return to, to
	default:
		return nil, nil
	}
}

// windowOverlaps reports whether the incident validity window
// [start_time, end_time] overlaps the requested [from, to]. A missing end
// time is open-ended into the future; a missing window bound is open-ended
// too. An unparsable start time (the model always serialises RFC 3339)
// never matches a bounded window.
func windowOverlaps(startTime string, endTime *string, from, to *time.Time) bool {
	start, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return false
	}
	var end *time.Time
	if endTime != nil {
		parsed, err := time.Parse(time.RFC3339, *endTime)
		if err != nil {
			return false
		}
		end = &parsed
	}
	if from != nil && end != nil && end.Before(*from) {
		return false
	}
	if to != nil && start.After(*to) {
		return false
	}
	return true
}

// setLower lowercases every value for case-insensitive comparisons
// (severity, type, province folding happens in model.ProvinceEqual).
func setLower(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[strings.ToLower(v)] = true
	}
	return out
}

// sortMatches sorts in place (the slice is private to Apply). All sorts are
// stable: equal keys keep the input order. Apply truncates to Limit
// afterwards, so the limit always applies to the sorted result.
func sortMatches(matches []model.Incident, sortKey string) {
	switch sortKey {
	case "severity":
		sort.SliceStable(matches, func(a, b int) bool {
			return severityRank(strings.ToLower(matches[a].Severity)) < severityRank(strings.ToLower(matches[b].Severity))
		})
	case "pk":
		sort.SliceStable(matches, func(a, b int) bool {
			pa, pb := pkSortKey(matches[a].PK), pkSortKey(matches[b].PK)
			if pa == nil {
				return false // null last
			}
			if pb == nil {
				return true
			}
			return *pa < *pb
		})
	case "start":
		sort.SliceStable(matches, func(a, b int) bool {
			ta, tb := parseStart(matches[a].StartTime), parseStart(matches[b].StartTime)
			if ta == nil {
				return false // unparsable last, deterministically
			}
			if tb == nil {
				return true
			}
			return ta.Before(*tb)
		})
	}
}

// pkSortKey is the ascending sort key of a PK: the start bound, or the end
// bound for a point location that only carries an end. nil sorts last.
func pkSortKey(pk *model.PK) *float64 {
	if pk == nil {
		return nil
	}
	if pk.From != nil {
		return pk.From
	}
	return pk.To
}

func parseStart(startTime string) *time.Time {
	t, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return nil
	}
	return &t
}
