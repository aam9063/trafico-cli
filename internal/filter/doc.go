// Package filter applies the common query filters of ARRANQUE section 5.2
// (road, PK range, direction, province, type, severity, status, date window,
// limit, sort) over in-memory incidents. Pure logic: no IO, no HTTP, no
// printing, no cobra. See filter.go for the rules this package fixes where
// section 5.2 left room (PK overlap, direction "both", unlocated
// composition, default status, limit validation, sort ties).
//
// Date contract: Criteria.From and Criteria.To are instants, while section
// 5.2 shows dates. The caller owns the date-to-instant parsing and must
// pass an inclusive end-of-day for To; a To at midnight of the last day
// silently drops everything that happens after midnight that day (for
// example a weekend query with --to 2026-10-31 excludes an incident
// starting at 2026-10-31T08:00). The overlap semantics are unchanged.
//
// meta.unlocated composition: the CLI applies the radius first
// (internal/geo), then Apply, and adds the geo count to Result.Unlocated,
// so meta.unlocated means "rows that would have matched but could not be
// placed".
package filter
