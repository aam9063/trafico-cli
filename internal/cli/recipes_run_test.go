package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/agentdocs"
	"github.com/aam9063/trafico-cli/internal/errs"
)

// newRecipesCLIServer serves the real fixtures augmented so every recipe of
// docs/ARRANQUE.md §6.4 has something to find (ODD ola-5-agentes T4): an
// active A-7 incident inside pk 100-140, planned N-332 roadworks starting
// the coming Saturday, and the A-6 panel of the vms fixture relocated to
// the AP-7 in the positive direction the recipe asks for. The rest of each
// fixture is served as-is.
func newRecipesCLIServer(t *testing.T, now time.Time) *httptest.Server {
	t.Helper()
	situations := recipesSituationsFixture(t, now)
	locations := recipesLocationsFixture(t)
	messages := vmsCLIFixture(t, "../../testdata/feeds/vms-messages/snap-1.xml")
	mux := http.NewServeMux()
	mux.HandleFunc("/situations", func(w http.ResponseWriter, r *http.Request) { w.Write(situations) })
	mux.HandleFunc("/vms-locations", func(w http.ResponseWriter, r *http.Request) { w.Write(locations) })
	mux.HandleFunc("/vms-messages", func(w http.ResponseWriter, r *http.Request) { w.Write(messages) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// recipesSituationsFixture appends the two synthetic situations the recipes
// need to the real trimmed snapshot: one active A-7 roadworks inside pk
// 100-140 (recipe estado-carretera and the route segment of mi-ruta) and
// one planned N-332 roadworks starting on the coming Saturday (recipe
// obras-fin-de-semana). The synthetic XML follows the measured shape of
// docs/feeds.md §4.1.
func recipesSituationsFixture(t *testing.T, now time.Time) []byte {
	t.Helper()
	body := string(cliSnapshot(t))
	if !strings.Contains(body, "</d2:payload>") {
		t.Fatal("the fixture snapshot has no closing d2:payload tag")
	}
	active := recipesSyntheticRoadworks("88800001", "A-7", 105, 120, now.AddDate(0, 0, -7), time.Time{})
	sat, sun := nextWeekend(now)
	start := time.Date(sat.Year(), sat.Month(), sat.Day(), 9, 0, 0, 0, now.Location())
	planned := recipesSyntheticRoadworks("88800002", "N-332", 30, 40, start, sun.AddDate(0, 0, 1))
	return []byte(strings.Replace(body, "</d2:payload>", active+"\n"+planned+"\n</d2:payload>", 1))
}

// nextWeekend returns the coming Saturday and its Sunday: the following one
// when today already is Saturday, so the planned roadworks always start in
// the future and computeStatus labels them planned.
func nextWeekend(now time.Time) (time.Time, time.Time) {
	days := (6 - int(now.Weekday()) + 7) % 7
	if days == 0 {
		days = 7
	}
	sat := now.AddDate(0, 0, days)
	return sat, sat.AddDate(0, 0, 1)
}

// recipesLocationsFixture relocates the A-6 panel of the real vms fixture
// to the AP-7 in the positive direction the paneles recipe asks for, so
// the recipe finds exactly one panel.
func recipesLocationsFixture(t *testing.T) []byte {
	t.Helper()
	body := vmsCLIFixture(t, "../../testdata/feeds/vms-locations/snap-1.xml")
	const device = `id="167937"`
	idx := strings.Index(string(body), device)
	if idx < 0 {
		t.Fatal("the vms fixture has no device 167937 to relocate")
	}
	tail := string(body[idx:])
	tail = strings.Replace(tail, "<loc:roadName>A-6</loc:roadName>", "<loc:roadName>AP-7</loc:roadName>", 1)
	tail = strings.Replace(tail,
		"<lse:tpegDirectionRoad>negative</lse:tpegDirectionRoad>",
		"<lse:tpegDirectionRoad>positive</lse:tpegDirectionRoad>", 1)
	return []byte(string(body[:idx]) + tail)
}

// recipesSyntheticRoadworks renders one roadworks situation record in the
// measured shape of docs/feeds.md §4.1: road, kilometre range, start and
// end instants (a zero end publishes no overallEndTime, so the incident
// stays active).
func recipesSyntheticRoadworks(id, road string, fromPK, toPK float64, start, end time.Time) string {
	endTag := ""
	if !end.IsZero() {
		endTag = "\n                    <com:overallEndTime>" + end.UTC().Format("2006-01-02T15:04:05.000Z07:00") + "</com:overallEndTime>"
	}
	startStr := start.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	return fmt.Sprintf(`
    <sit:situation id="%s">
        <sit:headerInformation>
            <com:informationStatus>real</com:informationStatus>
        </sit:headerInformation>
        <sit:situationRecord xsi:type="sit:Roadworks" id="%s" version="1">
            <sit:situationRecordCreationTime>%s</sit:situationRecordCreationTime>
            <sit:situationRecordVersionTime>%s</sit:situationRecordVersionTime>
            <sit:probabilityOfOccurrence>certain</sit:probabilityOfOccurrence>
            <sit:source>
                <com:sourceIdentification>DGT</com:sourceIdentification>
            </sit:source>
            <sit:validity>
                <com:validityStatus>active</com:validityStatus>
                <com:validityTimeSpecification>
                    <com:overallStartTime>%s</com:overallStartTime>%s
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
                        <loc:roadName>%s</loc:roadName>
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
                                <lse:kilometerPoint>%v</lse:kilometerPoint>
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
                                <lse:kilometerPoint>%v</lse:kilometerPoint>
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
    </sit:situation>`,
		id, id, startStr, startStr,
		startStr, endTag,
		road, toPK, fromPK)
}

// recipeArgv returns the argv of the recipe with its date placeholders
// substituted: the exact command an agent would run for the coming weekend.
func recipeArgv(recipe agentdocs.Recipe, now time.Time) []string {
	sat, sun := nextWeekend(now)
	argv := make([]string, 0, len(recipe.Command))
	for _, token := range recipe.Command[1:] {
		switch token {
		case "<sab>":
			token = sat.Format("2006-01-02")
		case "<dom>":
			token = sun.Format("2006-01-02")
		}
		argv = append(argv, token)
	}
	return argv
}

// TestRecipesRunAgainstFixtures executes every embedded recipe's command
// through the CLI harness against the fixtures (ODD ola-5-agentes T4, S3):
// each one must exit 0 and return a non-empty result. This is what proves
// the four recipes of docs/ARRANQUE.md §6.4 work against the binary.
func TestRecipesRunAgainstFixtures(t *testing.T) {
	setCLIEnv(t)
	now := time.Now()
	srv := newRecipesCLIServer(t, now)
	writeRecipesConfig(t, srv.URL)

	// The mi-ruta recipe checks the saved route "trabajo": create it the
	// same way an agent would, with the segment the recipe's output
	// describes.
	code, _, stderr := runCLI(t, "route", "add", "trabajo", "--segment", "A-7:100-140")
	if code != errs.ExitSuccess {
		t.Fatalf("route add trabajo exited %d, want 0 (stderr: %s)", code, stderr.String())
	}

	for _, recipe := range agentdocs.Recipes() {
		t.Run(recipe.ID, func(t *testing.T) {
			args := recipeArgv(recipe, now)
			code, stdout, stderr := runCLI(t, args...)
			if code != errs.ExitSuccess {
				t.Fatalf("%v exited %d, want 0 (stderr: %s)", args, code, stderr.String())
			}
			doc := decodeJSON(t, stdout)
			if _, ok := doc["attribution"].(map[string]any); !ok {
				t.Error("the document carries no attribution block (P-05)")
			}
			meta, ok := doc["meta"].(map[string]any)
			if !ok {
				t.Fatalf("the document carries no meta block: %v", doc)
			}
			if _, ok := meta["published_at"]; !ok {
				t.Error("meta carries no published_at key (P-08)")
			}
			// The recipe must resolve to a non-empty result: the fixture
			// carries data for every recipe's query.
			switch recipe.ID {
			case "estado-carretera", "obras-fin-de-semana":
				assertNonEmptyArray(t, doc, "incidents")
			case "mi-ruta":
				segments, ok := doc["segments"].([]any)
				if !ok || len(segments) == 0 {
					t.Fatalf("the document carries no segments array: %v", doc)
				}
				found := false
				for _, item := range segments {
					segment, _ := item.(map[string]any)
					if incidents, ok := segment["incidents"].([]any); ok && len(incidents) > 0 {
						found = true
					}
				}
				if !found {
					t.Errorf("no segment of the route carries incidents: %v", doc["segments"])
				}
			case "paneles":
				assertNonEmptyArray(t, doc, "signs")
			default:
				t.Fatalf("unknown recipe id %q: the run harness must cover every recipe", recipe.ID)
			}
		})
	}
}

// assertNonEmptyArray reports whether doc carries the key as a non-empty
// JSON array.
func assertNonEmptyArray(t *testing.T, doc map[string]any, key string) {
	t.Helper()
	items, ok := doc[key].([]any)
	if !ok {
		t.Fatalf("the document carries no %s array: %v", key, doc)
	}
	if len(items) == 0 {
		t.Errorf("%s is empty: the recipe resolved to nothing against the fixture", key)
	}
}

// writeRecipesConfig enables the situations and signs feeds pointed at the
// test server, the only feeds the four recipes touch.
func writeRecipesConfig(t *testing.T, srvURL string) {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := fmt.Sprintf(`[feeds.dgt-situations]
url = "%s/situations"
enabled = true

[feeds.dgt-signs]
url = "%s/vms-locations"
enabled = true

[feeds.dgt-signs-messages]
url = "%s/vms-messages"
enabled = true
`, srvURL, srvURL, srvURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
}
