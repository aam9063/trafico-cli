package watch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/store"
)

// deliveryContext is the background context of the sink tests.
var deliveryContext = context.Background()

// testEvent builds a pending event whose payload renders into a message with
// road, PK, type and severity.
func testEvent(t *testing.T, st *store.Store, kind string) store.Event {
	t.Helper()
	from, to := 102.3, 108.0
	data, err := marshalPayload(payload{
		Kind:       kind,
		IncidentID: "dgt:SIT-123",
		Road:       "A-7",
		PKFrom:     &from,
		PKTo:       &to,
		Type:       "roadworks",
		Severity:   "medium",
	})
	if err != nil {
		t.Fatalf("marshalPayload: %v", err)
	}
	events := []store.Event{{
		WatchName:  "trabajo", // storage metadata, never sent (P-07)
		IncidentID: "dgt:SIT-123",
		Kind:       kind,
		Payload:    string(data),
		CreatedAt:  checkNow,
	}}
	if err := st.AppendEvents(events); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	return events[0]
}

// requestRecord is what the test server hands to its recorder.
type requestRecord struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
}

// recordingBody is a response body that records whether it was drained (read
// to EOF) and closed, so the tests can pin that a delivery always closes the
// body on every path.
type recordingBody struct {
	readSoFar int
	drained   bool
	closed    bool
}

func (b *recordingBody) Read(p []byte) (int, error) {
	const payload = "ok"
	if b.readSoFar >= len(payload) {
		b.drained = true
		return 0, io.EOF
	}
	n := copy(p, payload[b.readSoFar:])
	b.readSoFar += n
	return n, nil
}

func (b *recordingBody) Close() error {
	b.closed = true
	return nil
}

// bodyRecordingTransport answers every request with a 200 whose body records
// Close, without touching the network.
type bodyRecordingTransport struct {
	bodies []*recordingBody
}

func (tr *bodyRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := &recordingBody{}
	tr.bodies = append(tr.bodies, body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{},
		Body:       body,
		Request:    req,
	}, nil
}

// testServer is an httptest server that records the requests it receives and
// answers with Status (0 means 200).
type testServer struct {
	HTTP   *httptest.Server
	Status int
	record func(requestRecord)
}

// newTestServer starts a recording server for the duration of the test.
func newTestServer(t *testing.T, record func(requestRecord)) *testServer {
	t.Helper()
	server := &testServer{record: record}
	server.HTTP = httptest.NewServer(handlerOf(server))
	t.Cleanup(server.HTTP.Close)
	return server
}

// handlerOf builds the recording handler of a test server.
func handlerOf(server *testServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		server.record(requestRecord{
			Method:      r.Method,
			Path:        r.URL.Path,
			ContentType: r.Header.Get("Content-Type"),
			Body:        body,
		})
		status := server.Status
		if status == 0 {
			status = 200
		}
		w.WriteHeader(status)
	})
}

// Client returns the HTTP client of the test server, so the sinks under test
// reach it without touching the network.
func (s *testServer) Client() *http.Client {
	return s.HTTP.Client()
}

// TestNtfySinkPostsMessageToTopic covers S8: the ntfy sink POSTs the message
// to <url>/<topic>.
func TestNtfySinkPostsMessageToTopic(t *testing.T) {
	var got requestRecord
	server := newTestServer(t, func(rec requestRecord) { got = rec })
	sink := &NtfySink{BaseURL: server.HTTP.URL, Topic: "topico-largo-y-aleatorio", Client: server.Client()}

	const message = "Nueva incidencia: obras en A-7 PK 102.3-108, severidad media. Fuente: DGT (CC BY)"
	if err := sink.Deliver(deliveryContext, message); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got.Method != "POST" {
		t.Errorf("method = %q, want POST", got.Method)
	}
	if got.Path != "/topico-largo-y-aleatorio" {
		t.Errorf("path = %q, want /topico-largo-y-aleatorio", got.Path)
	}
	if !strings.HasPrefix(got.ContentType, "text/plain") {
		t.Errorf("content type = %q, want text/plain", got.ContentType)
	}
	if string(got.Body) != message {
		t.Errorf("body = %q, want %q", got.Body, message)
	}
}

// TestWebhookSinkBodyShapes covers S8: each webhook template posts its own
// JSON body shape (Discord and Slack a text payload, Telegram chat_id+text).
func TestWebhookSinkBodyShapes(t *testing.T) {
	cases := []struct {
		name     string
		style    WebhookStyle
		chatID   string
		wantKeys []string
	}{
		{name: "discord", style: StyleDiscord, wantKeys: []string{"content"}},
		{name: "slack", style: StyleSlack, wantKeys: []string{"text"}},
		{name: "telegram", style: StyleTelegram, chatID: "12345678", wantKeys: []string{"chat_id", "text"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got requestRecord
			server := newTestServer(t, func(rec requestRecord) { got = rec })
			sink := &WebhookSink{URL: server.HTTP.URL, Style: tc.style, ChatID: tc.chatID, Client: server.Client()}
			if err := sink.Deliver(deliveryContext, "mensaje de prueba"); err != nil {
				t.Fatalf("Deliver: %v", err)
			}
			if !strings.HasPrefix(got.ContentType, "application/json") {
				t.Errorf("content type = %q, want application/json", got.ContentType)
			}
			var body map[string]any
			if err := json.Unmarshal(got.Body, &body); err != nil {
				t.Fatalf("body is not JSON: %v (%q)", err, got.Body)
			}
			if len(body) != len(tc.wantKeys) {
				t.Errorf("body has keys %v, want exactly %v", body, tc.wantKeys)
			}
			for _, key := range tc.wantKeys {
				if _, ok := body[key]; !ok {
					t.Errorf("body lacks key %q: %s", key, got.Body)
				}
			}
			for _, key := range tc.wantKeys {
				if key == "chat_id" {
					continue
				}
				if body[key] != "mensaje de prueba" {
					t.Errorf("body[%q] = %v, want the message", key, body[key])
				}
			}
			if tc.style == StyleTelegram && body["chat_id"] != "12345678" {
				t.Errorf("body[chat_id] = %v, want 12345678", body["chat_id"])
			}
		})
	}
}

// TestWebhookSinkTelegramRequiresChatID keeps a misconfigured Telegram sink
// from posting a body Telegram would reject: a missing chat_id is a delivery
// error, so the event stays undelivered instead of being lost.
func TestWebhookSinkTelegramRequiresChatID(t *testing.T) {
	server := newTestServer(t, func(rec requestRecord) {})
	sink := &WebhookSink{URL: server.HTTP.URL, Style: StyleTelegram, Client: server.Client()}
	if err := sink.Deliver(deliveryContext, "mensaje de prueba"); err == nil {
		t.Fatalf("telegram Deliver without chat_id = nil error, want an error")
	}
}

// TestWebhookSinkUnknownStyle rejects a style that is not one of the three
// templates, so a config typo cannot silently post a wrong body.
func TestWebhookSinkUnknownStyle(t *testing.T) {
	server := newTestServer(t, func(rec requestRecord) {})
	sink := &WebhookSink{URL: server.HTTP.URL, Style: "irc", Client: server.Client()}
	if err := sink.Deliver(deliveryContext, "mensaje de prueba"); err == nil {
		t.Fatalf("unknown style Deliver = nil error, want an error")
	}
}

// TestExecSinkDeliversMessageOnStdin covers S8: the exec sink runs a local
// command with the message on stdin.
func TestExecSinkDeliversMessageOnStdin(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "stdin.txt")
	helper := writeExecHelper(t, dir)

	sink := &ExecSink{Command: "go", Args: []string{"run", helper, outFile}}
	const message = "Nueva incidencia: obras en A-7 PK 102.3-108, severidad media. Fuente: DGT (CC BY)"
	if err := sink.Deliver(deliveryContext, message); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("helper did not write the message: %v", err)
	}
	if string(got) != message {
		t.Errorf("stdin = %q, want %q", got, message)
	}
}

// TestDeliverOneMarksDeliveredOnSuccess: after a 2xx the event is marked
// delivered and leaves the pending queue.
func TestDeliverOneMarksDeliveredOnSuccess(t *testing.T) {
	st := openTestStore(t)
	server := newTestServer(t, func(rec requestRecord) {})
	event := testEvent(t, st, "incident.new")

	sink := &NtfySink{BaseURL: server.HTTP.URL, Topic: "topico", Client: server.Client()}
	if err := DeliverOne(deliveryContext, st, event, sink); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending events after success = %d, want 0", len(pending))
	}
}

// TestFailedDeliveryLeavesEventUndelivered is the retry contract: a 500 from
// the sink must NOT mark the event delivered, so it can be retried later.
func TestFailedDeliveryLeavesEventUndelivered(t *testing.T) {
	st := openTestStore(t)
	server := newTestServer(t, func(rec requestRecord) {})
	server.Status = 500
	event := testEvent(t, st, "incident.new")

	sink := &NtfySink{BaseURL: server.HTTP.URL, Topic: "topico", Client: server.Client()}
	if err := DeliverOne(deliveryContext, st, event, sink); err == nil {
		t.Fatalf("DeliverOne against a 500 sink = nil error, want a delivery failure")
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != event.ID || pending[0].DeliveredAt != nil {
		t.Fatalf("pending events = %+v, want the event still undelivered", pending)
	}
}

// TestFailedExecDeliveryLeavesEventUndelivered: a non-zero exit of the exec
// command is a delivery failure, not a panic, and the event stays undelivered.
func TestFailedExecDeliveryLeavesEventUndelivered(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	helper := writeExecHelper(t, dir)
	event := testEvent(t, st, "incident.new")

	sink := &ExecSink{Command: "go", Args: []string{"run", helper, filepath.Join(dir, "out.txt"), "fail"}}
	if err := DeliverOne(deliveryContext, st, event, sink); err == nil {
		t.Fatalf("DeliverOne with a failing command = nil error, want a delivery failure")
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != event.ID || pending[0].DeliveredAt != nil {
		t.Fatalf("pending events = %+v, want the event still undelivered", pending)
	}
}

// TestDrainPendingDeliversOldestFirst covers the retry loop: pending events
// drain in order and each success is marked individually.
func TestDrainPendingDeliversOldestFirst(t *testing.T) {
	st := openTestStore(t)
	server := newTestServer(t, func(rec requestRecord) {})
	testEvent(t, st, "incident.new")
	testEvent(t, st, "incident.updated")

	sink := &NtfySink{BaseURL: server.HTTP.URL, Topic: "topico", Client: server.Client()}
	delivered, err := DrainPending(deliveryContext, st, sink, 0)
	if err != nil {
		t.Fatalf("DrainPending: %v", err)
	}
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2", delivered)
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending events = %d, want 0", len(pending))
	}
}

// TestDrainPendingStopsOnFailure pins that a failing sink stops the drain and
// reports the failure, leaving the rest queued for the next retry.
func TestDrainPendingStopsOnFailure(t *testing.T) {
	st := openTestStore(t)
	server := newTestServer(t, func(rec requestRecord) {})
	server.Status = 500
	testEvent(t, st, "incident.new")
	testEvent(t, st, "incident.updated")

	sink := &NtfySink{BaseURL: server.HTTP.URL, Topic: "topico", Client: server.Client()}
	delivered, err := DrainPending(deliveryContext, st, sink, 0)
	if err == nil {
		t.Fatalf("DrainPending with a failing sink = nil error, want a delivery failure")
	}
	if delivered != 0 {
		t.Fatalf("delivered = %d, want 0 (the first event already failed)", delivered)
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending events = %d, want 2 (nothing may be lost)", len(pending))
	}
}

// TestDeliverOneRejectsUndeliverablePayload: a payload that cannot render is
// an error and leaves the event pending rather than sending garbage.
func TestDeliverOneRejectsUndeliverablePayload(t *testing.T) {
	st := openTestStore(t)
	server := newTestServer(t, func(rec requestRecord) {})
	event := testEvent(t, st, "incident.new")
	event.Payload = `{not json`

	sink := &NtfySink{BaseURL: server.HTTP.URL, Topic: "topico", Client: server.Client()}
	if err := DeliverOne(deliveryContext, st, event, sink); err == nil {
		t.Fatalf("DeliverOne with a malformed payload = nil error, want an error")
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending events = %d, want 1", len(pending))
	}
}

// TestSinkDeliverClosesResponseBodyOnSuccess covers the sink delivery
// contract: on a 2xx response the body must be drained and closed, so the
// underlying connection goes back to the pool. A process notifying every
// five minutes indefinitely must not leak one connection per delivery.
func TestSinkDeliverClosesResponseBodyOnSuccess(t *testing.T) {
	cases := []struct {
		name    string
		deliver func(*http.Client) error
	}{
		{"ntfy", func(c *http.Client) error {
			return (&NtfySink{BaseURL: "https://ntfy.example", Topic: "topico", Client: c}).Deliver(deliveryContext, "mensaje de prueba")
		}},
		{"webhook", func(c *http.Client) error {
			return (&WebhookSink{URL: "https://hook.example", Style: StyleDiscord, Client: c}).Deliver(deliveryContext, "mensaje de prueba")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := &bodyRecordingTransport{}
			if err := tc.deliver(&http.Client{Transport: transport}); err != nil {
				t.Fatalf("Deliver: %v", err)
			}
			if len(transport.bodies) != 1 {
				t.Fatalf("deliveries = %d, want 1", len(transport.bodies))
			}
			if !transport.bodies[0].closed {
				t.Error("the response body was never closed: a successful notification leaks the connection")
			}
			if !transport.bodies[0].drained {
				t.Error("the response body was not drained: the connection cannot be reused")
			}
		})
	}
}

// TestDrainPendingSkipsUnrenderableEvent covers the poison-event rule of the
// retry loop: a payload that cannot be rendered is undeliverable data, not
// an outage, so the drain skips it (it stops blocking the queue forever) and
// still delivers every event behind it. A sink failure keeps the opposite
// behaviour, pinned by TestDrainPendingStopsOnFailure.
func TestDrainPendingSkipsUnrenderableEvent(t *testing.T) {
	st := openTestStore(t)
	server := newTestServer(t, func(rec requestRecord) {})

	// The unrenderable payload is the oldest pending event, so without the
	// rule it would block the two deliverable ones behind it forever.
	if err := st.AppendEvents([]store.Event{{
		WatchName:  "trabajo", // storage metadata, never sent (P-07)
		IncidentID: "dgt:SIT-bad",
		Kind:       "incident.new",
		Payload:    `{not json`,
		CreatedAt:  checkNow,
	}}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	testEvent(t, st, "incident.new")
	testEvent(t, st, "incident.updated")

	sink := &NtfySink{BaseURL: server.HTTP.URL, Topic: "topico", Client: server.Client()}
	delivered, err := DrainPending(deliveryContext, st, sink, 0)
	if err != nil {
		t.Fatalf("DrainPending: %v", err)
	}
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2 (the unrenderable event must not block the rest)", delivered)
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending events = %d, want 0 (the unrenderable event must stop blocking)", len(pending))
	}
}

// writeExecHelper writes a small Go program into dir that copies its stdin to
// the file given as first argument, or exits 1 when the second argument is
// "fail". It is run with `go run`, so the exec tests work on every CI
// operating system without shell-specific commands.
func writeExecHelper(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "stdin_helper.go")
	program := `package main

import (
	"io"
	"os"
)

func main() {
	if len(os.Args) >= 3 && os.Args[2] == "fail" {
		os.Exit(1)
	}
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(os.Args[1], data, 0o600); err != nil {
		os.Exit(4)
	}
}
`
	if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
		t.Fatalf("write exec helper: %v", err)
	}
	return path
}
