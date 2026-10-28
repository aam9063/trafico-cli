// Package errs defines the stable exit codes of trafico (ARRANQUE section 6.3)
// and the JSON error envelope that carries them. CORE-01 owns the envelope.
package errs

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
