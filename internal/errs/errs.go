// Package errs defines the stable exit codes of trafico (ARRANQUE section 6.3)
// and the JSON error envelope that carries them. CORE-01 owns the envelope.
package errs

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
)

// ExitCode is the process exit code of a trafico command.
type ExitCode int

// Stable exit codes, per ARRANQUE section 6.3. There is no code 3:
// there is no authentication.
const (
	// ExitSuccess: the command succeeded (also with stale cache; check meta.stale).
	ExitSuccess ExitCode = 0
	// ExitInternal: internal, an unexpected error.
	ExitInternal ExitCode = 1
	// ExitUsage: usage, cancelled or confirmation_required (for example --pk without --road).
	ExitUsage ExitCode = 2
	// ExitNotFound: not_found, an incident, route, place or watch does not exist.
	ExitNotFound ExitCode = 4
	// ExitUpstream: rate_limited or upstream_unavailable (429 or 5xx from the DGT).
	ExitUpstream ExitCode = 5
	// ExitNetwork: network or feed_unavailable, no network and no local cache.
	ExitNetwork ExitCode = 6
	// ExitFeedChanged: feed_changed, the URL disappeared (404) or the XML has an unexpected shape.
	ExitFeedChanged ExitCode = 7
	// ExitInterrupted: the process was interrupted (SIGINT/SIGTERM).
	ExitInterrupted ExitCode = 130
)

// issueBaseURL is the prefilled GitHub issue opened on exit code 7.
const issueBaseURL = "https://github.com/aam9063/trafico-cli/issues/new"

// Error is the error type of trafico: a stable type, a user-facing Spanish
// message, the exit code it maps to (ARRANQUE section 6.3) and, for errors
// tied to one dataset, the feed it came from. The JSON tags and their order
// are the envelope contract: do not rename or reorder them.
type Error struct {
	Type     string   `json:"type"`
	Message  string   `json:"message"`
	ExitCode ExitCode `json:"exit_code"`
	Feed     string   `json:"feed,omitempty"`
}

// Error implements the error interface; the message is user-facing Spanish.
func (e *Error) Error() string {
	return e.Message
}

// NewInternal reports an unexpected internal error (exit code 1).
func NewInternal(message string) *Error {
	return &Error{Type: "internal", Message: message, ExitCode: ExitInternal}
}

// NewUsage reports invalid arguments (exit code 2).
func NewUsage(message string) *Error {
	return &Error{Type: "usage", Message: message, ExitCode: ExitUsage}
}

// NewCancelled reports a command cancelled by the user (exit code 2).
func NewCancelled(message string) *Error {
	return &Error{Type: "cancelled", Message: message, ExitCode: ExitUsage}
}

// NewConfirmationRequired reports a local deletion attempted without --yes
// (exit code 2).
func NewConfirmationRequired(message string) *Error {
	return &Error{Type: "confirmation_required", Message: message, ExitCode: ExitUsage}
}

// NewNotFound reports an incident, route, place or watch that does not exist
// (exit code 4).
func NewNotFound(message string) *Error {
	return &Error{Type: "not_found", Message: message, ExitCode: ExitNotFound}
}

// NewRateLimited reports a 429 from the DGT server (exit code 5).
func NewRateLimited(message, feed string) *Error {
	return &Error{Type: "rate_limited", Message: message, ExitCode: ExitUpstream, Feed: feed}
}

// NewUpstreamUnavailable reports a 5xx from the DGT server (exit code 5).
func NewUpstreamUnavailable(message, feed string) *Error {
	return &Error{Type: "upstream_unavailable", Message: message, ExitCode: ExitUpstream, Feed: feed}
}

// NewNetwork reports a network failure without cache (exit code 6).
func NewNetwork(message string) *Error {
	return &Error{Type: "network", Message: message, ExitCode: ExitNetwork}
}

// NewFeedUnavailable reports no network and no local cache for a feed
// (exit code 6).
func NewFeedUnavailable(message, feed string) *Error {
	return &Error{Type: "feed_unavailable", Message: message, ExitCode: ExitNetwork, Feed: feed}
}

// NewFeedChanged reports that the DGT feed changed shape or disappeared
// (exit code 7). The message embeds a prefilled issue URL built with
// url.Values so the query is properly escaped.
func NewFeedChanged(detail string) *Error {
	values := url.Values{}
	values.Set("title", "La DGT ha cambiado el feed")
	values.Set("body", "Detalle: "+detail)
	issueURL := issueBaseURL + "?" + values.Encode()
	message := detail + " Abre una issue prellenada para avisarnos: " + issueURL
	return &Error{Type: "feed_changed", Message: message, ExitCode: ExitFeedChanged}
}

// ExitCodeFor returns the exit code of an *Error, and ExitInternal for any
// other error.
func ExitCodeFor(err error) ExitCode {
	var target *Error
	if errors.As(err, &target) {
		return target.ExitCode
	}
	return ExitInternal
}

// WriteError writes exactly one line of JSON, shaped
// {"error":{"type":"...","message":"...","exit_code":N}}, to w. The feed key
// is included only when the error carries a feed. An error that is not an
// *Error becomes an internal envelope; nil writes nothing.
func WriteError(w io.Writer, err error) error {
	if err == nil {
		return nil
	}
	var target *Error
	if !errors.As(err, &target) {
		target = &Error{Type: "internal", Message: err.Error(), ExitCode: ExitInternal}
	}
	data, mErr := json.Marshal(struct {
		Error *Error `json:"error"`
	}{target})
	if mErr != nil {
		return mErr
	}
	data = append(data, '\n')
	_, wErr := w.Write(data)
	return wErr
}
