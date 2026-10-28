// Package geo provides the distance layer of the query filters: haversine
// distance, radius filtering over in-memory incidents, and the resolution of
// the --near / --lat / --lon / --radius combination of ARRANQUE section 5.2.
// It is pure logic and never geocodes against external services: the only
// place names it accepts are the ones the user saved in config.Places
// ("lat,lon"), so no saved location ever leaves the machine (P-07).
//
// The exact combination rule ResolveTarget implements, stated per the task:
//
//   - A radius filter needs a target: --radius without --near and without
//     --lat/--lon is a usage error (exit code 2), and --radius must be
//     strictly positive. A negative --radius is a usage error even without
//     a target: silently ignoring the flag and running without a geo filter
//     would be worse than rejecting it. Radius 0 with no target keeps
//     meaning "no geo filter": nil coordinates, nil error.
//   - --near alone (with --radius) is allowed; it resolves through the
//     saved places and an unknown or malformed place is a usage error.
//   - --lat/--lon together (with --radius) are allowed without --near.
//   - --near and any of --lat/--lon together is a usage error: the user
//     must pick one origin. Partial coordinates (one of the two missing)
//     are a usage error too.
//   - No target and no radius means "no geo filter": nil coordinates, nil
//     error.
//
// Radius filtering keeps the incidents whose start point lies within the
// radius, boundary inclusive. An incident without coordinates never matches
// and is counted in the same Unlocated bucket as the province-less rows of
// section 5.2; the caller adds that count to filter.Result.Unlocated so
// meta.unlocated carries both.
package geo

import (
	"math"
	"strconv"
	"strings"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
)

// earthRadiusKm is the mean Earth radius used by the haversine formula.
const earthRadiusKm = 6371.0088

// Haversine returns the great-circle distance between two WGS84 positions
// in kilometres, straight line, no route calculation (ARRANQUE section 5.2:
// --near uses straight-line distance on the feed coordinates).
func Haversine(a, b model.Coordinates) float64 {
	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	dLat := lat2 - lat1
	dLon := (b.Longitude - a.Longitude) * math.Pi / 180
	sinDLat := math.Sin(dLat / 2)
	sinDLon := math.Sin(dLon / 2)
	h := sinDLat*sinDLat + math.Cos(lat1)*math.Cos(lat2)*sinDLon*sinDLon
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(h)))
}

// FilterRadius keeps the incidents whose start point is within radiusKm of
// centre, boundary inclusive, and returns them in input order together with
// the count of dropped incidents that carry no coordinates (the same
// Unlocated bucket as the province-less rows). It never mutates its input;
// an incident without a start point never matches a radius filter.
func FilterRadius(incidents []model.Incident, centre model.Coordinates, radiusKm float64) (kept []model.Incident, unlocated int) {
	kept = make([]model.Incident, 0, len(incidents))
	for _, inc := range incidents {
		if inc.Location.Start == nil {
			unlocated++
			continue
		}
		if Haversine(centre, *inc.Location.Start) <= radiusKm {
			kept = append(kept, inc)
		}
	}
	return kept, unlocated
}

// ResolvePlace resolves a saved place name through the config places map
// (config.Places: name -> "lat,lon") into coordinates. An unknown place, a
// malformed value or out-of-range coordinates are usage errors (exit code
// 2), never a network lookup: there is no geocoder (P-07, D-03).
func ResolvePlace(name string, places map[string]string) (model.Coordinates, error) {
	value, ok := places[name]
	if !ok {
		return model.Coordinates{}, errs.NewUsage("el lugar \"" + name + "\" no está guardado; usa trafico place add")
	}
	return parseCoordinates(value)
}

// ResolveTarget validates the --near / --lat / --lon / --radius combination
// and resolves it to the centre of the radius filter, or nil when no geo
// filter was requested. See the package comment for the exact rule; every
// violation is a usage error (exit code 2). A negative radius is rejected
// even when no target was given; radius 0 with no target means no geo
// filter.
func ResolveTarget(near string, lat, lon *float64, radiusKm float64, places map[string]string) (*model.Coordinates, error) {
	if radiusKm < 0 {
		return nil, errs.NewUsage("--radius debe ser estrictamente positivo")
	}
	if radiusKm == 0 {
		if near == "" && lat == nil && lon == nil {
			return nil, nil // no geo filter requested
		}
		return nil, errs.NewUsage("--near y --lat/--lon requieren un --radius positivo")
	}
	if near != "" {
		if lat != nil || lon != nil {
			return nil, errs.NewUsage("--near y --lat/--lon son excluyentes: elige un origen")
		}
		centre, err := ResolvePlace(near, places)
		if err != nil {
			return nil, err
		}
		return &centre, nil
	}
	if lat == nil || lon == nil {
		return nil, errs.NewUsage("--radius necesita --near o bien --lat y --lon juntos")
	}
	if err := validateRange(*lat, *lon); err != nil {
		return nil, err
	}
	return &model.Coordinates{Latitude: *lat, Longitude: *lon}, nil
}

// parseCoordinates parses a config place value "lat,lon" (spaces around the
// parts tolerated) and validates the ranges.
func parseCoordinates(value string) (model.Coordinates, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return model.Coordinates{}, errs.NewUsage("el lugar debe guardarse como \"lat,lon\", por ejemplo \"38.3452,-0.4810\"")
	}
	lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, errLon := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if errLat != nil || errLon != nil {
		return model.Coordinates{}, errs.NewUsage("el lugar debe guardarse como \"lat,lon\", por ejemplo \"38.3452,-0.4810\"")
	}
	if err := validateRange(lat, lon); err != nil {
		return model.Coordinates{}, err
	}
	return model.Coordinates{Latitude: lat, Longitude: lon}, nil
}

func validateRange(lat, lon float64) error {
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return errs.NewUsage("las coordenadas están fuera de rango: latitud -90..90, longitud -180..180")
	}
	return nil
}
