package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/errs"
)

// incidentsBody serves the snap-1 snapshot as-is.
func incidentsBody(t *testing.T) []byte {
	t.Helper()
	return cliSnapshot(t)
}

// fixtureWithEveningIncident appends one synthetic situation (N-332, planned
// roadworks starting 2026-11-02T20:00 Madrid time) to the snap-1 snapshot,
// so the end-of-day rule of --to has a row to catch. The synthetic XML
// follows the measured shape of docs/feeds.md §4.1.
func fixtureWithEveningIncident(t *testing.T) []byte {
	t.Helper()
	const eveningSituation = `
    <sit:situation id="99990001">
        <sit:headerInformation>
            <com:informationStatus>real</com:informationStatus>
        </sit:headerInformation>
        <sit:situationRecord xsi:type="sit:Roadworks" id="99990002" version="1">
            <sit:situationRecordCreationTime>2026-10-20T10:00:00.000+01:00</sit:situationRecordCreationTime>
            <sit:situationRecordVersionTime>2026-10-20T10:00:00.000+01:00</sit:situationRecordVersionTime>
            <sit:probabilityOfOccurrence>certain</sit:probabilityOfOccurrence>
            <sit:source>
                <com:sourceIdentification>DGT</com:sourceIdentification>
            </sit:source>
            <sit:validity>
                <com:validityStatus>active</com:validityStatus>
                <com:validityTimeSpecification>
                    <com:overallStartTime>2026-11-02T20:00:00.000+01:00</com:overallStartTime>
                    <com:overallEndTime>2026-11-03T06:00:00.000+01:00</com:overallEndTime>
                </com:validityTimeSpecification>
            </sit:validity>
            <sit:cause>
                <sit:causeType>roadMaintenance</sit:causeType>
                <sit:detailedCauseType>
                    <sit:roadMaintenanceType>roadworks</sit:roadMaintenanceType>
                </sit:detailedCauseType>
            </sit:cause>
            <sit:locationReference xsi:type="loc:SingleRoadLinearLocation">
                <loc:supplementaryPositionalDescription>
                    <loc:roadInformation>
                        <loc:roadName>N-332</loc:roadName>
                    </loc:roadInformation>
                </loc:supplementaryPositionalDescription>
                <loc:tpegLinearLocation>
                    <loc:tpegDirection>southWestBound</loc:tpegDirection>
                    <loc:tpegLinearLocationType>segment</loc:tpegLinearLocationType>
                    <loc:to xsi:type="loc:TpegNonJunctionPoint">
                        <loc:pointCoordinates>
                            <loc:latitude>38.20</loc:latitude>
                            <loc:longitude>-0.60</loc:longitude>
                        </loc:pointCoordinates>
                        <loc:_tpegNonJunctionPointExtension>
                            <loc:extendedTpegNonJunctionPoint>
                                <lse:kilometerPoint>80.5</lse:kilometerPoint>
                                <lse:municipality>Elche</lse:municipality>
                                <lse:province>Alacant/Alicante</lse:province>
                            </loc:extendedTpegNonJunctionPoint>
                        </loc:_tpegNonJunctionPointExtension>
                    </loc:to>
                    <loc:from xsi:type="loc:TpegNonJunctionPoint">
                        <loc:pointCoordinates>
                            <loc:latitude>38.10</loc:latitude>
                            <loc:longitude>-0.55</loc:longitude>
                        </loc:pointCoordinates>
                        <loc:_tpegNonJunctionPointExtension>
                            <loc:extendedTpegNonJunctionPoint>
                                <lse:kilometerPoint>75.0</lse:kilometerPoint>
                                <lse:municipality>Elche</lse:municipality>
                                <lse:province>Alacant/Alicante</lse:province>
                            </loc:extendedTpegNonJunctionPoint>
                        </loc:_tpegNonJunctionPointExtension>
                    </loc:from>
                    <loc:_tpegLinearLocationExtension>
                        <loc:extendedTpegLinearLocation>
                            <lse:tpegDirectionRoad>negative</lse:tpegDirectionRoad>
                        </loc:extendedTpegLinearLocation>
                    </loc:_tpegLinearLocationExtension>
                </loc:tpegLinearLocation>
            </sit:locationReference>
            <sit:roadMaintenanceType>roadworks</sit:roadMaintenanceType>
        </sit:situationRecord>
    </sit:situation>`
	body := string(incidentsBody(t))
	if !strings.Contains(body, "</d2:payload>") {
		t.Fatal("the fixture snapshot has no closing d2:payload tag")
	}
	return []byte(strings.Replace(body, "</d2:payload>", eveningSituation+"\n</d2:payload>", 1))
}

// newIncidentsServer serves body on /situations for the incidents tests.
func newIncidentsServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/situations", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// eveningIncidentPresent reports whether the stdout document includes the
// synthetic 2026-11-02 20:00 incident.
func eveningIncidentPresent(t *testing.T, stdout []byte) bool {
	t.Helper()
	return strings.Contains(string(stdout), "dgt:99990002")
}

// TestIncidentsPKWithoutRoadIsUsage pins ARRANQUE §6.3: --pk without exactly
// one --road exits 2 with a usage envelope on stderr and an empty stdout,
// and it spends no request on the feed.
func TestIncidentsPKWithoutRoadIsUsage(t *testing.T) {
	setCLIEnv(t)
	srv, hits := newCLIServer(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "incidents", "--pk", "100-140")
	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage)", code, errs.ExitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &doc); err != nil {
		t.Fatalf("stderr is not one JSON envelope: %v\nstderr: %q", err, stderr.String())
	}
	errObj, _ := doc["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("stderr carries no error envelope: %v", doc)
	}
	if errObj["type"] != "usage" {
		t.Errorf("error.type = %v, want usage", errObj["type"])
	}
	if errObj["exit_code"] != float64(errs.ExitUsage) {
		t.Errorf("error.exit_code = %v, want 2", errObj["exit_code"])
	}
	if got := hits("/situations"); got != 0 {
		t.Errorf("invalid criteria made %d requests, want 0", got)
	}
}

// Non-finite geo values (pflag and strconv both accept "NaN" and "inf")
// must be rejected as usage errors before any request reaches the feed:
// a NaN radius or centre silently matches nothing and the response would
// claim an honest "no incidents" (CMD-01 review, blocker 1).
func TestIncidentsNonFiniteGeoValuesAreUsage(t *testing.T) {
	setCLIEnv(t)
	srv, hits := newCLIServer(t)
	writeCLIConfig(t, srv.URL)

	tests := []struct {
		name string
		args []string
	}{
		{name: "NaN latitude", args: []string{"incidents", "--radius", "15", "--lat", "NaN", "--lon", "-0.5"}},
		{name: "NaN longitude", args: []string{"incidents", "--radius", "15", "--lat", "38", "--lon", "NaN"}},
		{name: "NaN radius", args: []string{"incidents", "--radius", "NaN", "--lat", "38", "--lon", "-0.5"}},
		{name: "inf radius", args: []string{"incidents", "--radius", "inf", "--lat", "38", "--lon", "-0.5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.args...)
			if code != errs.ExitUsage {
				t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout must stay empty on a usage error, got %q", stdout.String())
			}
			var doc map[string]any
			if err := json.Unmarshal(stderr.Bytes(), &doc); err != nil {
				t.Fatalf("stderr is not one JSON envelope: %v\nstderr: %q", err, stderr.String())
			}
			errObj, _ := doc["error"].(map[string]any)
			if errObj == nil || errObj["type"] != "usage" {
				t.Errorf("error envelope = %v, want type usage", doc["error"])
			}
		})
	}
	if got := hits("/situations"); got != 0 {
		t.Errorf("invalid geo values made %d requests, want 0", got)
	}
}

// TestIncidentsToEndOfDayIncludesSameEvening pins the date contract of
// --to: a bare date means the end of that day inclusive
// (23:59:59.999999999 Europe/Madrid), so an incident starting that evening
// at 20:00 stays inside the window, and the previous day alone does not
// catch it.
func TestIncidentsToEndOfDayIncludesSameEvening(t *testing.T) {
	setCLIEnv(t)
	srv := newIncidentsServer(t, fixtureWithEveningIncident(t))
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "incidents", "--status", "all", "--from", "2026-11-02", "--to", "2026-11-02")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !eveningIncidentPresent(t, stdout.Bytes()) {
		t.Fatalf("--to 2026-11-02 must include the incident starting that day at 20:00; stdout: %s", stdout.String())
	}

	code, stdout, _ = runCLI(t, "incidents", "--status", "all", "--from", "2026-11-01", "--to", "2026-11-01")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if eveningIncidentPresent(t, stdout.Bytes()) {
		t.Fatal("--to 2026-11-01 must not include an incident starting 2026-11-02 at 20:00")
	}
}

// TestIncidentsPrettyEndsWithAttribution pins the CC BY closing line of the
// pretty renderer (ARRANQUE §2.1).
func TestIncidentsPrettyEndsWithAttribution(t *testing.T) {
	setCLIEnv(t)
	srv := newIncidentsServer(t, incidentsBody(t))
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "incidents", "--pretty")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.HasSuffix(stdout.String(), "Fuente: DGT (CC BY)\n") {
		t.Fatalf("pretty output must end with the attribution line; got %q", stdout.String())
	}
}

// TestIncidentsStdoutIsOneJSONValue pins P-01: the default output is exactly
// one JSON value carrying the envelope keys of §5.3, attribution and
// meta.published_at included, plus the coverage note of an unfiltered query.
func TestIncidentsStdoutIsOneJSONValue(t *testing.T) {
	setCLIEnv(t)
	srv := newIncidentsServer(t, incidentsBody(t))
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "incidents", "--road", "a62")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if _, ok := doc["attribution"]; !ok {
		t.Error("the document carries no attribution block (P-05)")
	}
	meta, ok := doc["meta"].(map[string]any)
	if !ok {
		t.Fatalf("the document carries no meta object: %v", doc)
	}
	if _, ok := meta["published_at"]; !ok {
		t.Error("meta carries no published_at (P-08)")
	}
	if note, _ := meta["coverage_note"].(string); note == "" {
		t.Error("an unfiltered query must carry meta.coverage_note")
	}
	if road, ok := doc["query"].(map[string]any)["road"].([]any); !ok || len(road) != 1 || road[0] != "A-62" {
		t.Errorf("query.road = %v, want the normalised [\"A-62\"]", doc["query"])
	}
	incidents, ok := doc["incidents"].([]any)
	if !ok || len(incidents) != 2 {
		t.Errorf("incidents = %v, want the 2 A-62 records", doc["incidents"])
	}
}

// TestIncidentsShowOutputsTheIncident pins the show command end to end: one
// incident by id on stdout, and exit 4 with a JSON envelope for an absent id.
func TestIncidentsShowOutputsTheIncident(t *testing.T) {
	setCLIEnv(t)
	srv := newIncidentsServer(t, incidentsBody(t))
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "incidents", "show", "dgt:18811074")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	incidents, ok := doc["incidents"].([]any)
	if !ok || len(incidents) != 1 {
		t.Fatalf("show must return exactly one incident, got %v", doc["incidents"])
	}
	first, _ := incidents[0].(map[string]any)
	if first["id"] != "dgt:18811074" {
		t.Errorf("show returned id %v, want dgt:18811074", first["id"])
	}

	code, stdout, stderr = runCLI(t, "incidents", "show", "dgt:does-not-exist")
	if code != errs.ExitNotFound {
		t.Fatalf("exit code = %d, want 4 (not_found)", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty on a not-found error, got %q", stdout.String())
	}
	var doc2 map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &doc2); err != nil {
		t.Fatalf("stderr is not one JSON envelope: %v", err)
	}
	errObj, _ := doc2["error"].(map[string]any)
	if errObj == nil || errObj["type"] != "not_found" {
		t.Errorf("error envelope = %v, want type not_found", doc2["error"])
	}
}
