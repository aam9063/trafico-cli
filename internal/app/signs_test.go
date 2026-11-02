package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
)

// vmsLocationsBody loads the real trimmed DevicePublication snapshot
// (public CC BY data) for the test server.
func vmsLocationsBody(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/vms-locations/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the vms-locations snapshot: %v", err)
	}
	return data
}

// vmsMessagesBody loads the real trimmed VmsPublication snapshot.
func vmsMessagesBody(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/vms-messages/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the vms-messages snapshot: %v", err)
	}
	return data
}

// testSignsConfig builds a configuration with both sign feeds enabled and
// pointed at the test server.
func testSignsConfig(srvURL string) *config.Config {
	return &config.Config{
		Feeds: map[string]config.Feed{
			config.FeedSigns:         {URL: srvURL + "/vms-locations", Enabled: true},
			config.FeedSignsMessages: {URL: srvURL + "/vms-messages", Enabled: true},
		},
		Stale: config.Stale{MaxAge: config.DefaultMaxAge},
	}
}

func newSignsFetcher(t *testing.T, srvURL string, now time.Time) (*config.Config, *feeds.Fetcher) {
	t.Helper()
	cfg := testSignsConfig(srvURL)
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")
	fetcher.Now = func() time.Time { return now }
	return cfg, fetcher
}

// newSignsFixtureServer serves both real snapshots on their paths and
// reports how many requests each received, so tests can prove which feeds
// were (never) fetched. No test ever touches the real network (AGENTS.md).
func newSignsFixtureServer(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()
	locations, messages := vmsLocationsBody(t), vmsMessagesBody(t)
	srv, counts := newFeedServer(t, map[string]http.HandlerFunc{
		"/vms-locations": func(w http.ResponseWriter, r *http.Request) { w.Write(locations) },
		"/vms-messages":  func(w http.ResponseWriter, r *http.Request) { w.Write(messages) },
	})
	return srv, counts
}

// messagesFixtureWithOrphan appends one synthetic controller (id 999999,
// with a real message) that has no matching device, so the join can prove
// that an orphan is reported instead of silently dropped.
func messagesFixtureWithOrphan(t *testing.T) []byte {
	t.Helper()
	const orphan = `
    <vms:vmsControllerStatus>
        <vms:vmsControllerReference targetClass="vms:VmsController" id="999999"/>
        <vms:vmsStatus vmsIndex="1">
            <vms:vmsStatus>
                <vms:vmsMessage messageIndex="1">
                    <vms:vmsMessage>
                        <vms:timeLastSet>2026-10-28T12:55:31.000+01:00</vms:timeLastSet>
                        <vms:displayAreaSettings displayAreaIndex="1">
                            <vms:displayAreaSettings xsi:type="vms:TextDisplay">
                                <vms:textLine lineIndex="1">
                                    <vms:textLine><vms:textLine>OBRAS</vms:textLine></vms:textLine>
                                </vms:textLine>
                            </vms:displayAreaSettings>
                        </vms:displayAreaSettings>
                    </vms:vmsMessage>
                </vms:vmsMessage>
            </vms:vmsStatus>
        </vms:vmsStatus>
    </vms:vmsControllerStatus>`
	return bytes.Replace(vmsMessagesBody(t), []byte("</d2:payload>"), []byte(orphan+"\n</d2:payload>"), 1)
}

// messagesFixtureWithDuplicate appends one synthetic controller that
// repeats the id of a real device (167938) with a different message, so the
// join can prove that a duplicated controller id is reported on the stderr
// channel and that the payload's last block still wins.
func messagesFixtureWithDuplicate(t *testing.T) []byte {
	t.Helper()
	const duplicate = `
    <vms:vmsControllerStatus>
        <vms:vmsControllerReference targetClass="vms:VmsController" id="167938"/>
        <vms:vmsStatus vmsIndex="9">
            <vms:vmsStatus>
                <vms:vmsMessage messageIndex="1">
                    <vms:vmsMessage>
                        <vms:timeLastSet>2026-10-28T13:55:31.000+01:00</vms:timeLastSet>
                        <vms:displayAreaSettings displayAreaIndex="1">
                            <vms:displayAreaSettings xsi:type="vms:TextDisplay">
                                <vms:textLine lineIndex="1">
                                    <vms:textLine><vms:textLine>DUPLICADO</vms:textLine></vms:textLine>
                                </vms:textLine>
                            </vms:displayAreaSettings>
                        </vms:displayAreaSettings>
                    </vms:vmsMessage>
                </vms:vmsMessage>
            </vms:vmsStatus>
        </vms:vmsStatus>
    </vms:vmsControllerStatus>`
	return bytes.Replace(vmsMessagesBody(t), []byte("</d2:payload>"), []byte(duplicate+"\n</d2:payload>"), 1)
}

// TestSignsGolden pins the full envelope of `trafico signs` over the real
// fixtures, byte-compared against internal/app/testdata/signs.golden.json.
func TestSignsGolden(t *testing.T) {
	srv, _ := newSignsFixtureServer(t)
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	response, err := Signs(context.Background(), cfg, fetcher, SignCriteria{})
	if err != nil {
		t.Fatalf("Signs failed: %v", err)
	}
	var buf bytes.Buffer
	if err := output.Write(&buf, response, output.Options{}); err != nil {
		t.Fatalf("serialising the response: %v", err)
	}
	got := buf.Bytes()
	goldenPath := "testdata/signs.golden.json"
	if *update {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with go test ./internal/app/ -update): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestSignsJoinsMessagesToDevices pins the join of docs/feeds.md §5: every
// controller id that has a device is matched (61441 shows its message), a
// device without messages stays with an empty messages array (167938 is off,
// 167937 has no controller entry), both feeds are fetched exactly once and
// published_at comes from the messages feed.
func TestSignsJoinsMessagesToDevices(t *testing.T) {
	srv, counts := newSignsFixtureServer(t)
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	response, err := Signs(context.Background(), cfg, fetcher, SignCriteria{})
	if err != nil {
		t.Fatalf("Signs failed: %v", err)
	}
	if got := counts("/vms-locations"); got != 1 {
		t.Errorf("locations feed fetched %d times, want 1", got)
	}
	if got := counts("/vms-messages"); got != 1 {
		t.Errorf("messages feed fetched %d times, want 1", got)
	}
	if len(response.Signs) != 5 {
		t.Fatalf("Signs returned %d signs, want 5", len(response.Signs))
	}
	if response.Meta.Total != 5 {
		t.Errorf("meta.total = %d, want 5", response.Meta.Total)
	}
	wantPublished := time.Date(2026, 10, 28, 12, 4, 37, 0, time.UTC)
	if response.Meta.PublishedAt == nil || !response.Meta.PublishedAt.Equal(wantPublished) {
		t.Errorf("meta.published_at = %v, want %v (the messages feed)", response.Meta.PublishedAt, wantPublished)
	}

	byID := map[string]int{}
	for i, sign := range response.Signs {
		byID[sign.ID] = i
	}
	active := response.Signs[byID["dgt:61441"]]
	if len(active.Messages) != 1 {
		t.Fatalf("dgt:61441 messages = %+v, want one", active.Messages)
	}
	if len(active.Messages[0].TextLines) != 1 || active.Messages[0].TextLines[0] != "VELOCIDAD CONTROLADA POR RADAR" {
		t.Errorf("dgt:61441 text lines = %q, want the rendered text", active.Messages[0].TextLines)
	}
	if len(active.Messages[0].Pictograms) != 2 {
		t.Errorf("dgt:61441 pictograms = %q, want two in display-area order", active.Messages[0].Pictograms)
	}
	if active.MessageUpdatedAt == nil || *active.MessageUpdatedAt != "2026-10-28T11:55:31Z" {
		t.Errorf("dgt:61441 message_updated_at = %v, want 2026-10-28T11:55:31Z", active.MessageUpdatedAt)
	}

	off := response.Signs[byID["dgt:167938"]]
	if off.Messages == nil || len(off.Messages) != 0 {
		t.Errorf("dgt:167938 messages = %+v, want an empty non-nil array (off panel)", off.Messages)
	}
	if off.MessageUpdatedAt != nil {
		t.Errorf("dgt:167938 message_updated_at = %v, want nil", off.MessageUpdatedAt)
	}

	noMessage := response.Signs[byID["dgt:167937"]]
	if noMessage.Messages == nil || len(noMessage.Messages) != 0 {
		t.Errorf("dgt:167937 messages = %+v, want an empty non-nil array (no controller entry)", noMessage.Messages)
	}
}

// TestSignsRoadFilterMatchesNormalised pins --road on signs: the filter
// compares normalised road names, so "a54" matches the feed's A-54, and an
// unknown road is an empty array, never an error.
func TestSignsRoadFilterMatchesNormalised(t *testing.T) {
	srv, _ := newSignsFixtureServer(t)
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	matches, err := Signs(context.Background(), cfg, fetcher, SignCriteria{Roads: []string{"a54"}})
	if err != nil {
		t.Fatalf("Signs failed: %v", err)
	}
	if len(matches.Signs) != 1 || matches.Signs[0].ID != "dgt:167938" || matches.Signs[0].Road != "A-54" {
		t.Fatalf("Signs = %+v, want only dgt:167938 on A-54", matches.Signs)
	}

	unknown, err := Signs(context.Background(), cfg, fetcher, SignCriteria{Roads: []string{"ZZ-99"}})
	if err != nil {
		t.Fatalf("Signs with an unknown road failed: %v", err)
	}
	if unknown.Signs == nil || len(unknown.Signs) != 0 {
		t.Errorf("Signs with an unknown road = %+v, want an empty non-nil array", unknown.Signs)
	}
	if unknown.Meta.Total != 0 {
		t.Errorf("meta.total = %d, want 0", unknown.Meta.Total)
	}
}

// TestSignsProvinceFilterFoldsCase pins that the uppercase province of this
// feed (docs/feeds.md §5.1) matches a lowercase --province through
// model.ProvinceEqual.
func TestSignsProvinceFilterFoldsCase(t *testing.T) {
	srv, _ := newSignsFixtureServer(t)
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	matches, err := Signs(context.Background(), cfg, fetcher, SignCriteria{Provinces: []string{"lugo"}})
	if err != nil {
		t.Fatalf("Signs failed: %v", err)
	}
	if len(matches.Signs) != 3 {
		t.Fatalf("Signs = %d, want the 3 LUGO panels", len(matches.Signs))
	}
	for _, sign := range matches.Signs {
		if sign.Province != "LUGO" {
			t.Errorf("sign %s province = %q, want the source text LUGO", sign.ID, sign.Province)
		}
	}
}

// TestSignsDirectionFilter pins the shared direction semantics: positive
// matches only that direction of travel, and an unknown direction is a usage
// error rejected before any request reaches the feeds.
func TestSignsDirectionFilter(t *testing.T) {
	srv, counts := newSignsFixtureServer(t)
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	matches, err := Signs(context.Background(), cfg, fetcher, SignCriteria{Direction: "positive"})
	if err != nil {
		t.Fatalf("Signs failed: %v", err)
	}
	if len(matches.Signs) != 1 || matches.Signs[0].ID != "dgt:167939" {
		t.Fatalf("Signs = %+v, want only dgt:167939 (positive)", matches.Signs)
	}

	if _, err := Signs(context.Background(), cfg, fetcher, SignCriteria{Direction: "north"}); err == nil {
		t.Fatal("Signs accepted an unknown direction, want a usage error")
	} else if errs.ExitCodeFor(err) != errs.ExitUsage {
		t.Errorf("error = %v, want a usage error", err)
	}
	if got := counts("/vms-locations") + counts("/vms-messages"); got != 2 {
		// The valid call fetched each feed once; the invalid one spent 0.
		t.Errorf("invalid direction spent %d requests total, want 2 (only the valid call's)", got)
	}
}

// TestSignsDirectionBothMatchesOneWayFilter pins the direction semantics of
// docs/feeds.md §4.4 that the fixtures cannot show (no panel travels both
// ways in the snapshot): a sign travelling in both directions matches a
// one-way filter instead of being excluded by it.
func TestSignsDirectionBothMatchesOneWayFilter(t *testing.T) {
	both := "both"
	matches, unlocated := filterSigns([]model.Sign{{ID: "dgt:1", Direction: &both}}, SignCriteria{Direction: "positive"})
	if len(matches) != 1 || matches[0].ID != "dgt:1" {
		t.Fatalf("filterSigns with direction positive = %+v, want the both-direction sign", matches)
	}
	if unlocated != 0 {
		t.Errorf("unlocated = %d, want 0", unlocated)
	}

	negative := "negative"
	matches, _ = filterSigns([]model.Sign{{ID: "dgt:2", Direction: &negative}}, SignCriteria{Direction: "positive"})
	if len(matches) != 0 {
		t.Fatalf("filterSigns with direction positive = %+v, want the negative sign excluded", matches)
	}
}

// TestSignsNegativeLimitIsUsage pins that a negative --limit is rejected
// before any request reaches the feeds.
func TestSignsNegativeLimitIsUsage(t *testing.T) {
	srv, counts := newSignsFixtureServer(t)
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	if _, err := Signs(context.Background(), cfg, fetcher, SignCriteria{Limit: -1}); err == nil {
		t.Fatal("Signs accepted a negative limit, want a usage error")
	} else if errs.ExitCodeFor(err) != errs.ExitUsage {
		t.Errorf("error = %v, want a usage error", err)
	}
	if got := counts("/vms-locations") + counts("/vms-messages"); got != 0 {
		t.Errorf("invalid criteria made %d requests, want 0", got)
	}
}

// TestSignsLimitTruncates pins --limit on signs.
func TestSignsLimitTruncates(t *testing.T) {
	srv, _ := newSignsFixtureServer(t)
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	matches, err := Signs(context.Background(), cfg, fetcher, SignCriteria{Limit: 2})
	if err != nil {
		t.Fatalf("Signs failed: %v", err)
	}
	if len(matches.Signs) != 2 || matches.Meta.Total != 2 {
		t.Fatalf("Signs = %d signs (total %d), want 2 and 2", len(matches.Signs), matches.Meta.Total)
	}
}

// TestSignsOrphanControllerIsReported pins the join rule of the task: a
// message with no matching device is never silently dropped — it is reported
// on the stderr channel (P-01: stdout stays one pure JSON value) and the
// valid signs are unaffected.
func TestSignsOrphanControllerIsReported(t *testing.T) {
	locations := vmsLocationsBody(t)
	messages := messagesFixtureWithOrphan(t)
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/vms-locations": func(w http.ResponseWriter, r *http.Request) { w.Write(locations) },
		"/vms-messages":  func(w http.ResponseWriter, r *http.Request) { w.Write(messages) },
	})
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	response, err := Signs(context.Background(), cfg, fetcher, SignCriteria{})
	if err != nil {
		t.Fatalf("Signs failed: %v", err)
	}
	if len(response.Signs) != 5 {
		t.Fatalf("Signs returned %d signs, want the 5 real devices (no orphan sign)", len(response.Signs))
	}
	if !strings.Contains(response.Warning, "999999") {
		t.Errorf("Warning = %q, want the orphan controller id 999999 reported", response.Warning)
	}
}

// TestSignsDuplicateControllerIsReported pins the join rule for duplicated
// controller ids (review FEED-03): the messages feed carrying the same
// vmsControllerReference id twice is not silently resolved to last-wins —
// the id is reported on the stderr channel (P-01) and the payload's last
// block still wins.
func TestSignsDuplicateControllerIsReported(t *testing.T) {
	locations := vmsLocationsBody(t)
	messages := messagesFixtureWithDuplicate(t)
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/vms-locations": func(w http.ResponseWriter, r *http.Request) { w.Write(locations) },
		"/vms-messages":  func(w http.ResponseWriter, r *http.Request) { w.Write(messages) },
	})
	cfg, fetcher := newSignsFetcher(t, srv.URL, goldenClock)

	response, err := Signs(context.Background(), cfg, fetcher, SignCriteria{})
	if err != nil {
		t.Fatalf("Signs failed: %v", err)
	}
	if !strings.Contains(response.Warning, "167938") {
		t.Errorf("Warning = %q, want the duplicated controller id 167938 reported", response.Warning)
	}
	for _, sign := range response.Signs {
		if sign.ID != "dgt:167938" {
			continue
		}
		if len(sign.Messages) != 1 || len(sign.Messages[0].TextLines) != 1 || sign.Messages[0].TextLines[0] != "DUPLICADO" {
			t.Errorf("dgt:167938 messages = %+v, want the last block's message DUPLICADO", sign.Messages)
		}
	}
}

// TestBuildSignsReportsOrphans is the unit-level pin of the join: one
// device, two controllers sharing its id, and one orphan id.
func TestBuildSignsReportsOrphans(t *testing.T) {
	devices := []datex2.VmsDevice{{ID: "10", TypeOfDevice: "vms", RoadName: "A-7"}}
	controllers := []datex2.VmsController{
		{ID: "10", Messages: []datex2.VmsMessage{{TextLines: []string{"OBRAS"}}}},
		{ID: "10", Messages: []datex2.VmsMessage{{TextLines: []string{"TRÁFICO"}}}},
		{ID: "99", Messages: []datex2.VmsMessage{{TextLines: []string{"HUÉRFANO"}}}},
	}
	signs, orphans, duplicates := BuildSigns(devices, controllers)
	if len(signs) != 1 || len(signs[0].Messages) != 1 || signs[0].Messages[0].TextLines[0] != "TRÁFICO" {
		t.Fatalf("signs = %+v, want one sign with the last message of its controller", signs)
	}
	if len(orphans) != 1 || orphans[0] != "99" {
		t.Fatalf("orphans = %q, want [99]", orphans)
	}
	if len(duplicates) != 1 || duplicates[0] != "10" {
		t.Errorf("duplicates = %q, want [10] (id 10 appears in two controllers)", duplicates)
	}
}

// TestBuildSignsReportsDuplicateControllers is the unit-level pin of the
// duplicate rule (review FEED-03): two controllers sharing one device id
// keep last-wins and the id is reported instead of being silently resolved.
func TestBuildSignsReportsDuplicateControllers(t *testing.T) {
	devices := []datex2.VmsDevice{
		{ID: "10", TypeOfDevice: "vms", RoadName: "A-7"},
		{ID: "20", TypeOfDevice: "vms", RoadName: "A-7"},
	}
	controllers := []datex2.VmsController{
		{ID: "10", Messages: []datex2.VmsMessage{{TextLines: []string{"PRIMERO"}}}},
		{ID: "10", Messages: []datex2.VmsMessage{{TextLines: []string{"ÚLTIMO"}}}},
		{ID: "20", Messages: []datex2.VmsMessage{{TextLines: []string{"ÚNICO"}}}},
	}
	signs, orphans, duplicates := BuildSigns(devices, controllers)
	if len(signs) != 2 {
		t.Fatalf("signs = %+v, want two signs (one per device)", signs)
	}
	if len(signs[0].Messages) != 1 || signs[0].Messages[0].TextLines[0] != "ÚLTIMO" {
		t.Fatalf("signs[0] = %+v, want the last block of the duplicated controller", signs[0])
	}
	if len(orphans) != 0 {
		t.Errorf("orphans = %q, want none", orphans)
	}
	if len(duplicates) != 1 || duplicates[0] != "10" {
		t.Fatalf("duplicates = %q, want [10]", duplicates)
	}
}
