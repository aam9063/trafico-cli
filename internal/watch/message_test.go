package watch

import (
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/store"
)

// The rendered notification is the user-facing surface of S7 and P-07: it
// carries only road, kilometre point, incident type, severity and the DGT
// attribution. It must never contain the watch name, a route name, a place
// name or coordinates.

// payloadJSON marshals a payload for the rendering tests.
func payloadJSON(t *testing.T, p payload) string {
	t.Helper()
	data, err := marshalPayload(p)
	if err != nil {
		t.Fatalf("marshalPayload: %v", err)
	}
	return string(data)
}

// assertExactMessage pins the exact user-facing wording.
func assertExactMessage(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("message =\n  %q\nwant\n  %q", got, want)
	}
}

// TestRenderMessageNewIncident pins the exact wording of an incident.new
// notification.
func TestRenderMessageNewIncident(t *testing.T) {
	from, to := 102.3, 108.0
	msg, err := RenderMessage(payloadJSON(t, payload{
		Kind:       "incident.new",
		IncidentID: "dgt:SIT-123",
		Road:       "A-7",
		PKFrom:     &from,
		PKTo:       &to,
		Type:       "roadworks",
		Severity:   "medium",
	}))
	if err != nil {
		t.Fatalf("RenderMessage: %v", err)
	}
	assertExactMessage(t, msg, "Nueva incidencia: obras en A-7 PK 102.3-108, severidad media. Fuente: DGT (CC BY)")
}

// TestRenderMessageUpdatedIncident pins the wording of an incident.updated
// notification.
func TestRenderMessageUpdatedIncident(t *testing.T) {
	from := 102.3
	msg, err := RenderMessage(payloadJSON(t, payload{
		Kind:       "incident.updated",
		IncidentID: "dgt:SIT-123",
		Road:       "A-7",
		PKFrom:     &from,
		Type:       "accident",
		Severity:   "high",
	}))
	if err != nil {
		t.Fatalf("RenderMessage: %v", err)
	}
	assertExactMessage(t, msg, "Incidencia actualizada: accidente en A-7 PK 102.3, severidad alta. Fuente: DGT (CC BY)")
}

// TestRenderMessageCleared pins the wording of an incident.cleared
// notification: the incident is gone from the feed, so the message carries
// the last known road, kilometre point, type and severity remembered in the
// store, plus the attribution — the user must be able to tell WHAT ended.
func TestRenderMessageCleared(t *testing.T) {
	from, to := 102.3, 108.0
	msg, err := RenderMessage(payloadJSON(t, payload{
		Kind:       "incident.cleared",
		IncidentID: "dgt:SIT-123",
		Road:       "A-7",
		PKFrom:     &from,
		PKTo:       &to,
		Type:       "roadworks",
		Severity:   "medium",
	}))
	if err != nil {
		t.Fatalf("RenderMessage: %v", err)
	}
	assertExactMessage(t, msg, "Incidencia finalizada: obras en A-7 PK 102.3-108, severidad media. Fuente: DGT (CC BY)")
}

// TestRenderMessageClearedOmitsUnknownFields covers P-04 on the cleared
// notification: whatever the stored state does not remember (type, road,
// kilometre point, severity) is omitted instead of printed as an empty
// placeholder.
func TestRenderMessageClearedOmitsUnknownFields(t *testing.T) {
	msg, err := RenderMessage(payloadJSON(t, payload{
		Kind:       "incident.cleared",
		IncidentID: "dgt:SIT-123",
		Road:       "N-340",
		Severity:   "unknown",
	}))
	if err != nil {
		t.Fatalf("RenderMessage: %v", err)
	}
	assertExactMessage(t, msg, "Incidencia finalizada: en N-340. Fuente: DGT (CC BY)")
}

// TestRenderMessageOmitsWhatTheFeedDoesNotCarry covers P-04 on the
// notification: an unknown severity is omitted, never invented, and an
// incident without kilometre point loses the PK segment.
func TestRenderMessageOmitsWhatTheFeedDoesNotCarry(t *testing.T) {
	msg, err := RenderMessage(payloadJSON(t, payload{
		Kind:       "incident.new",
		IncidentID: "dgt:SIT-123",
		Type:       "other",
		Severity:   "unknown",
	}))
	if err != nil {
		t.Fatalf("RenderMessage: %v", err)
	}
	assertExactMessage(t, msg, "Nueva incidencia: incidencia. Fuente: DGT (CC BY)")
}

// TestRenderMessageRejectsUnknownPayload keeps malformed payloads from
// becoming empty notifications: an unknown kind or malformed JSON is an
// internal error.
func TestRenderMessageRejectsUnknownPayload(t *testing.T) {
	if _, err := RenderMessage(`{"kind":"wat"}`); err == nil {
		t.Fatalf("RenderMessage with unknown kind = nil error, want an error")
	}
	if _, err := RenderMessage(`{not json`); err == nil {
		t.Fatalf("RenderMessage with malformed JSON = nil error, want an error")
	}
}

// TestRenderMessageRecognisesUnserialisablePayloadMarker pins that the
// marker kind the watch engine stores when a payload cannot be serialised is
// reported as an explicit internal error naming the failure — never rendered
// as a message and never silently turned into a generic unknown-kind error.
func TestRenderMessageRecognisesUnserialisablePayloadMarker(t *testing.T) {
	_, err := RenderMessage(`{"kind":"internal.unserialisable-payload"}`)
	if err == nil {
		t.Fatalf("RenderMessage of the unserialisable-payload marker = nil error, want an internal error")
	}
	if !strings.Contains(err.Error(), "no se pudo serializar") {
		t.Errorf("error %q does not name the serialisation failure", err)
	}
}

// TestPrivacyMessageNeverContainsWatchNameOrCoordinates is the S7/P-07 hard
// rule, exercised end to end through the engine: a watch named after the
// user's home (the name even embeds coordinates) watches an incident that
// carries coordinates, and the rendered notification contains neither the
// watch name nor any coordinate. The road, the PK, the type, the severity and
// the attribution must all be there.
func TestPrivacyMessageNeverContainsWatchNameOrCoordinates(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "Casa de Alberto 38.3452,-0.4810"}
	watcher := NewWatcher(st)

	// Baseline (silently stores the empty snapshot, thanks to the baseline
	// sentinel), then a second check where the incident appears: the event
	// that would be delivered is the one under test.
	if _, err := watcher.Check(def, nil, checkNow); err != nil {
		t.Fatalf("baseline Check: %v", err)
	}
	result, err := watcher.Check(def, []model.Incident{incidentFixture("SIT-1", "medium")}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("second Check: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(result.Events), result.Events)
	}

	msg, err := RenderMessage(result.Events[0].Payload)
	if err != nil {
		t.Fatalf("RenderMessage: %v", err)
	}

	// Never present: the watch name, its embedded coordinates, and the
	// coordinates of the incident itself.
	for _, forbidden := range []string{
		"Casa de Alberto",
		"38.3452", "-0.4810",
		"38.30", "-0.55",
	} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("message contains %q (P-07 violation): %q", forbidden, msg)
		}
	}
	// Always present: road, PK, type, severity and attribution.
	for _, required := range []string{"A-7", "102.3", "108", "obras", "severidad media", "Fuente: DGT (CC BY)"} {
		if !strings.Contains(msg, required) {
			t.Errorf("message lacks %q: %q", required, msg)
		}
	}

	// The same hard rule on the cleared event: after two consecutive
	// absences the notification still carries the last known road, PK, type
	// and severity, and never the watch name or a coordinate.
	if _, err := watcher.Check(def, nil, checkNow.Add(10*time.Minute)); err != nil {
		t.Fatalf("first absence Check: %v", err)
	}
	cleared, err := watcher.Check(def, nil, checkNow.Add(15*time.Minute))
	if err != nil {
		t.Fatalf("second absence Check: %v", err)
	}
	if len(cleared.Events) != 1 || cleared.Events[0].Kind != store.EventIncidentCleared {
		t.Fatalf("cleared events = %+v, want exactly one incident.cleared", cleared.Events)
	}
	clearedMsg, err := RenderMessage(cleared.Events[0].Payload)
	if err != nil {
		t.Fatalf("RenderMessage (cleared): %v", err)
	}
	for _, forbidden := range []string{
		"Casa de Alberto",
		"38.3452", "-0.4810",
		"38.30", "-0.55",
	} {
		if strings.Contains(clearedMsg, forbidden) {
			t.Errorf("cleared message contains %q (P-07 violation): %q", forbidden, clearedMsg)
		}
	}
	for _, required := range []string{"A-7", "102.3", "108", "obras", "severidad media", "Fuente: DGT (CC BY)"} {
		if !strings.Contains(clearedMsg, required) {
			t.Errorf("cleared message lacks %q: %q", required, clearedMsg)
		}
	}
}
