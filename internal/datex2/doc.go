// Package datex2 parses DGT DATEX II payloads into Go values. It is the only
// package in trafico-cli allowed to know the XML (AGENTS.md hard rule).
//
// Current scope (FEED-02): the incident feed, DATEX II v3.7 DGT RTTI profile
// (sit:SituationPublication, docs/feeds.md §4). ParseSituations streams the
// payload with xml.Decoder — the payload is ~7 MB and is decoded once per
// minute, so nothing reads the whole document into a buffer first. The
// package does not apply filters, does not build the final JSON model and
// does not fetch anything.
//
// Normalised type and severity resolution lives in mapping.go, driven by the
// embedded mapping.yaml. The raw DATEX II values are always preserved on the
// returned records so a wrong mapping never loses source text (P-04). Fields
// the feed does not publish are simply absent; none are invented.
//
// Namespaces are matched by URI, never by prefix: the DGT payload binds sit:,
// com:, loc: and lse: to the levelC/schema/3 namespaces, but any prefix is
// accepted.
package datex2
