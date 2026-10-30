// Package watch implements the watch engine of OLA-3 (WATCH-01c): it decides
// WHAT gets notified, given a watch definition and the incidents currently
// matching it. It is pure decision logic over internal/store, which persists,
// and internal/window, which parses the time windows: no cobra, no config
// files, no CLI, and no HTTP client of its own except the notification sinks
// themselves.
//
// # The pieces
//
//   - Watcher.Check (watch.go) runs one check of a Definition against the
//     current incidents at a given instant, persists the new snapshot and
//     returns the store events to deliver. Baseline, incident.new,
//     incident.updated (severity, lanes or end time changed) and
//     incident.cleared (two consecutive absences, via the store's
//     MissingCount) live here, as does the window gate: outside every window
//     the check does not run, stores nothing and reports Skipped instead of
//     inventing an event.
//
//   - RenderMessage (message.go) renders the user-facing Spanish text of an
//     event payload. The payload struct only has fields for road, kilometre
//     point, incident type and severity, so a notification CANNOT contain a
//     watch name, a route name, a place name or a coordinate (ARRANQUE P-07,
//     spec S7): the user's home and workplace never leave the machine. The
//     cleared message renders what the store still remembers about the
//     incident (road, kilometre point, type, severity), so it says WHAT
//     ended. Every message ends with the CC BY attribution line (P-05).
//
//   - The sinks (sinks.go) deliver a rendered message: NtfySink POSTs it to
//     <url>/<topic>, WebhookSink POSTs a JSON body shaped by its template
//     (Discord and Slack a text payload, Telegram chat_id+text), and ExecSink
//     runs a local command with the message on stdin. DeliverOne delivers one
//     event and, only on success, stamps delivered_at; any failure leaves the
//     event pending for the retry loop of DrainPending.
//
//   - ValidateInterval rejects a polling interval below MinInterval (2
//     minutes, default 5) with a usage error: the DGT updates the feed every
//     minute and asking more often is abuse, not vigilance (spec S6).
//
// # The baseline (spec S3) and why a sentinel exists
//
// A watch with no previous state records the current snapshot and emits
// NOTHING: the first run must never dump twenty "new incident" alerts for
// what was already happening when the user created the watch. The CLI-layer
// opt-in --emit-initial (WATCH-01d) is only possible because this baseline
// exists.
//
// store.PreviousState cannot distinguish "this watch never ran" from "its
// last check found zero incidents", and the difference matters: without a
// marker, the first real incident after an empty check would be silently
// swallowed by the baseline rule and could later be announced as
// incident.cleared without an incident.new ever having fired. Check
// therefore stores a sentinel state row (baselineKey, an id no contract
// incident can carry) to mark the watch as initialised. It never leaves this
// package and never reaches an event or a notification.
//
// # Delivery guarantees
//
// Check appends events to the log BEFORE replacing the snapshot, so a crash
// in between can at worst duplicate a notification on the next run, never
// silently lose one. A failed sink delivery (non-2xx response, failed exec
// command, unusable payload) leaves the event undelivered: store.DeliveredAt
// stays null and DrainPending retries oldest first.
//
// # Privacy
//
// The watch name travels only in store.Event.WatchName as storage metadata
// and is rejected by construction from every payload and message. No test and
// no code path in this package sends anything over the network except the
// sinks, which receive only the rendered message.
package watch
