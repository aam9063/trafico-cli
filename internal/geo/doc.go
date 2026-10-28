// Package geo provides the distance layer of the query filters: haversine
// distance, radius filtering and saved-place lookups. It never geocodes
// against external services: the only place names it resolves come from the
// user's own config, so saved locations never leave the machine (P-07).
// See geo.go for the exact --near / --lat / --lon / --radius rule.
//
// Radius rules: --radius must be strictly positive. A negative --radius is
// a usage error (exit code 2) even when no target was given — an ignored
// flag is worse than a rejected one. Radius 0 with no target means "no geo
// filter".
//
// Composition with internal/filter: the caller applies the radius first and
// adds the geo unlocated count (incidents without coordinates) to
// filter.Result.Unlocated, so meta.unlocated means "rows that would have
// matched but could not be placed".
package geo
