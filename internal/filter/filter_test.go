package filter

import (
	"errors"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
)

// --- builders ------------------------------------------------------------

type incOpt func(*model.Incident)

func i(opts ...incOpt) model.Incident {
	inc := model.Incident{
		SchemaVersion: model.SchemaVersion,
		ID:            "dgt:test",
		SituationID:   "1",
		Source:        model.Source,
		Type:          "accident",
		Severity:      "unknown",
		Status:        "active",
		Road:          "A-7",
		StartTime:     "2026-10-28T06:00:00Z",
		UpdatedAt:     "2026-10-28T09:29:00Z",
	}
	for _, opt := range opts {
		opt(&inc)
	}
	return inc
}

func withRoad(r string) incOpt { return func(x *model.Incident) { x.Road = r } }
func kind(t string) incOpt     { return func(x *model.Incident) { x.Type = t } }
func severity(s string) incOpt { return func(x *model.Incident) { x.Severity = s } }
func status(s string) incOpt   { return func(x *model.Incident) { x.Status = s } }
func province(p string) incOpt { return func(x *model.Incident) { x.Location.Province = p } }
func startAt(ts string) incOpt { return func(x *model.Incident) { x.StartTime = ts } }
func endAt(ts *string) incOpt  { return func(x *model.Incident) { x.EndTime = ts } }

func dir(d *string) incOpt {
	return func(x *model.Incident) { x.Direction = d }
}

func strPtr(s string) *string { return &s }

func flt(v float64) *float64 { return &v }

func interval(from, to *float64) incOpt {
	return func(x *model.Incident) { x.PK = &model.PK{From: from, To: to} }
}

// applyRuns collects the incident IDs of a Result in order.
func ids(r Result) []string {
	out := make([]string, 0, len(r.Incidents))
	for _, inc := range r.Incidents {
		out = append(out, inc.ID)
	}
	return out
}

func named(id string) incOpt { return func(x *model.Incident) { x.ID = id } }

func assertUsage(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Apply() = nil error, want usage error")
	}
	var target *errs.Error
	if !errors.As(err, &target) {
		t.Fatalf("Apply() error = %v, want *errs.Error", err)
	}
	if target.ExitCode != errs.ExitUsage {
		t.Errorf("exit code = %d, want %d (usage)", target.ExitCode, errs.ExitUsage)
	}
}

// --- road filter ---------------------------------------------------------

func TestRoadFilterMatchesAnyRoadWithRoadEquality(t *testing.T) {
	incidents := []model.Incident{i(withRoad("A-7"), named("a7")), i(withRoad("N-332a"), named("n332a"))}
	tests := []struct {
		name  string
		roads []string
		want  []string
	}{
		{name: "lowercase without separator", roads: []string{"a7"}, want: []string{"a7"}},
		{name: "space separator", roads: []string{"A 7"}, want: []string{"a7"}},
		{name: "lowercase hyphen", roads: []string{"a-7"}, want: []string{"a7"}},
		{name: "canonical hyphen", roads: []string{"A-7"}, want: []string{"a7"}},
		{name: "several roads", roads: []string{"AP-7", "n-332a"}, want: []string{"n332a"}},
		{name: "N-332 does not match N-332a", roads: []string{"N-332"}, want: nil},
		{name: "no roads matches everything", roads: nil, want: []string{"a7", "n332a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(incidents, Criteria{Roads: tt.roads, Status: "all"})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got, want := ids(got), tt.want; len(got) != len(want) {
				t.Errorf("matches = %v, want %v", got, want)
			} else {
				for k := range want {
					if got[k] != want[k] {
						t.Errorf("matches = %v, want %v", got, want)
						break
					}
				}
			}
		})
	}
}

// --- PK filter -----------------------------------------------------------

func TestPKRangeMatchesOnOverlapNotContainment(t *testing.T) {
	incidents := []model.Incident{i(interval(flt(95), flt(105)), named("span"))}
	tests := []struct {
		name  string
		from  *float64
		to    *float64
		match bool
	}{
		{name: "range starts inside the incident (task example 100-140)", from: flt(100), to: flt(140), match: true},
		{name: "range inside the incident", from: flt(98), to: flt(102), match: true},
		{name: "incident inside the range", from: flt(90), to: flt(110), match: true},
		{name: "range ends inside the incident", from: flt(80), to: flt(100), match: true},
		{name: "range entirely after", from: flt(106), to: flt(140), match: false},
		{name: "range entirely before", from: flt(60), to: flt(94.5), match: false},
		{name: "touching boundaries overlap", from: flt(105), to: flt(140), match: true},
		{name: "open-ended range up to", from: nil, to: flt(100), match: true},
		{name: "open-ended range past", from: nil, to: flt(90), match: false},
		{name: "open-ended range from", from: flt(100), to: nil, match: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(incidents, Criteria{Roads: []string{"A-7"}, PK: &Range{From: tt.from, To: tt.to}, Status: "all"})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if match := len(got.Incidents) == 1; match != tt.match {
				t.Errorf("matched = %v, want %v (matches: %v)", match, tt.match, ids(got))
			}
		})
	}
}

func TestPKPointIncidentMatches(t *testing.T) {
	// A point location serialises one bound as null (docs/feeds.md section 4.8).
	incidents := []model.Incident{i(interval(flt(102.3), nil), named("point"))}
	got, err := Apply(incidents, Criteria{Roads: []string{"A-7"}, PK: &Range{From: flt(100), To: flt(105)}, Status: "all"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Incidents) != 1 {
		t.Errorf("point incident not matched at its own kilometre point: %v", ids(got))
	}
}

func TestPKNilIncidentNeverMatches(t *testing.T) {
	incidents := []model.Incident{i(named("no-pk"))}
	got, err := Apply(incidents, Criteria{Roads: []string{"A-7"}, PK: &Range{From: flt(0), To: flt(1000)}, Status: "all"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Incidents) != 0 {
		t.Errorf("incident with nil PK matched a PK range: %v", ids(got))
	}
}

func TestPKWithoutExactlyOneRoadIsUsageError(t *testing.T) {
	incidents := []model.Incident{i()}
	tests := []struct {
		name  string
		roads []string
	}{
		{name: "no road", roads: nil},
		{name: "two roads", roads: []string{"A-7", "AP-7"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Apply(incidents, Criteria{Roads: tt.roads, PK: &Range{From: flt(100), To: flt(140)}})
			assertUsage(t, err)
		})
	}
}

func TestPKRangeWithInvertedBoundsIsUsageError(t *testing.T) {
	_, err := Apply([]model.Incident{i()}, Criteria{Roads: []string{"A-7"}, PK: &Range{From: flt(140), To: flt(100)}})
	assertUsage(t, err)
}

// --- direction filter ----------------------------------------------------

func TestDirectionFilter(t *testing.T) {
	nilDir := []model.Incident{
		i(dir(nil), named("nil")),
		i(dir(strPtr("positive")), named("pos")),
		i(dir(strPtr("negative")), named("neg")),
	}
	tests := []struct {
		name      string
		direction string
		want      []string
	}{
		{name: "unset matches everything including null direction", direction: "", want: []string{"nil", "pos", "neg"}},
		{name: "both matches everything including null direction", direction: "both", want: []string{"nil", "pos", "neg"}},
		{name: "positive excludes null direction", direction: "positive", want: []string{"pos"}},
		{name: "negative excludes null direction", direction: "negative", want: []string{"neg"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(nilDir, Criteria{Direction: tt.direction, Status: "all"})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got, want := ids(got), tt.want; len(got) != len(want) || got[0] != want[0] {
				t.Errorf("matches = %v, want %v", got, want)
			}
		})
	}
}

func TestDirectionUnknownValueIsUsageError(t *testing.T) {
	_, err := Apply([]model.Incident{i()}, Criteria{Direction: "west", Status: "all"})
	assertUsage(t, err)
}

// --- province filter -----------------------------------------------------

func TestProvinceFilterNormalisesAndCountsUnlocated(t *testing.T) {
	incidents := []model.Incident{
		i(province("Lugo"), named("lugo")),
		i(province("LUGO"), named("lugo-upper")),
		i(province("Alacant/Alicante"), named("alicante")),
		i(province(""), named("no-province")),
		i(province("Madrid"), named("madrid")),
	}
	got, err := Apply(incidents, Criteria{Provinces: []string{"LUGO", "alicante"}, Status: "all"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if want := []string{"lugo", "lugo-upper", "alicante"}; len(ids(got)) != 3 {
		t.Errorf("matches = %v, want %v", ids(got), want)
	}
	if got.Unlocated != 1 {
		t.Errorf("Unlocated = %d, want 1 (the incident with no province)", got.Unlocated)
	}
}

func TestProvinceUnlocatedZeroWithoutProvinceFilter(t *testing.T) {
	incidents := []model.Incident{i(province(""), named("no-province"))}
	got, err := Apply(incidents, Criteria{Status: "all"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got.Unlocated != 0 {
		t.Errorf("Unlocated = %d without a province filter, want 0", got.Unlocated)
	}
	if len(got.Incidents) != 1 {
		t.Errorf("incident without province dropped without a province filter: %v", ids(got))
	}
}

// --- type and severity filters -------------------------------------------

func TestTypeAndSeverityFiltersNormaliseLowercase(t *testing.T) {
	incidents := []model.Incident{
		i(kind("roadworks"), severity("high"), named("works")),
		i(kind("congestion"), severity("high"), named("jam")),
		i(kind("roadworks"), named("no-severity")),
	}
	got, err := Apply(incidents, Criteria{Types: []string{"Roadworks"}, Severities: []string{"HIGH"}, Status: "all"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Incidents) != 1 || got.Incidents[0].Type != "roadworks" {
		t.Errorf("matches = %v, want the mixed-case roadworks/high incident", ids(got))
	}
}

func TestSeverityUnknownMatchesExplicitUnknownFilter(t *testing.T) {
	incidents := []model.Incident{i(severity("unknown"), named("unpublished"))}
	got, err := Apply(incidents, Criteria{Severities: []string{"unknown"}, Status: "all"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Incidents) != 1 {
		t.Errorf("severity filter [unknown] did not match an incident the DGT publishes no severity for")
	}
}

// --- status filter -------------------------------------------------------

func TestStatusFilterDefaultsToActive(t *testing.T) {
	incidents := []model.Incident{
		i(status("active"), named("active")),
		i(status("planned"), named("planned")),
		i(status("ended"), named("ended")),
	}
	tests := []struct {
		name   string
		status string
		want   []string
	}{
		{name: "empty defaults to active", status: "", want: []string{"active"}},
		{name: "explicit active", status: "active", want: []string{"active"}},
		{name: "planned", status: "planned", want: []string{"planned"}},
		{name: "all", status: "all", want: []string{"active", "planned", "ended"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(incidents, Criteria{Status: tt.status})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got, want := ids(got), tt.want; len(got) != len(want) {
				t.Errorf("matches = %v, want %v", got, want)
			}
		})
	}
}

func TestStatusUnknownValueIsUsageError(t *testing.T) {
	_, err := Apply([]model.Incident{i()}, Criteria{Status: "finished"})
	assertUsage(t, err)
}

// --- date window ---------------------------------------------------------

func TestDateWindowMatchesOnOverlap(t *testing.T) {
	ended := "2026-10-28T14:00:00Z"
	incidents := []model.Incident{
		i(startAt("2026-10-31T08:00:00Z"), endAt(strPtr("2026-11-02T20:00:00Z")), named("weekend")),
		i(startAt("2026-10-31T08:00:00Z"), endAt(nil), named("open-ended")),
		i(startAt("2026-10-28T06:00:00Z"), endAt(strPtr(ended)), named("already")),
	}
	utc := func(y int, mo time.Month, d int) *time.Time {
		t := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
		return &t
	}
	tests := []struct {
		name string
		from *time.Time
		to   *time.Time
		want []string
	}{
		{name: "window inside the validity", from: utc(2026, 10, 31), to: utc(2026, 11, 1), want: []string{"weekend", "open-ended"}},
		{name: "window starts after the validity", from: utc(2026, 11, 3), to: utc(2026, 11, 5), want: []string{"open-ended"}},
		{name: "window ends before the validity", from: utc(2026, 10, 25), to: utc(2026, 10, 30), want: []string{"already"}},
		{name: "open window (only from)", from: utc(2026, 11, 1), to: nil, want: []string{"weekend", "open-ended"}},
		{name: "window ending at midnight excludes a start later that day (inclusive at the exact instant only)", from: nil, to: utc(2026, 10, 31), want: []string{"already"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(incidents, Criteria{Status: "all", From: tt.from, To: tt.to})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got, want := ids(got), tt.want; len(got) != len(want) {
				t.Errorf("matches = %v, want %v", got, want)
			}
		})
	}
}

func TestDateWindowInvertedBoundsIsUsageError(t *testing.T) {
	from := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	_, err := Apply([]model.Incident{i()}, Criteria{From: &from, To: &to})
	assertUsage(t, err)
}

// --- sort ----------------------------------------------------------------

func TestSortSeverityHighFirstUnknownLast(t *testing.T) {
	incidents := []model.Incident{
		i(severity("unknown"), named("u")),
		i(severity("low"), named("l")),
		i(severity("high"), named("h")),
		i(severity("medium"), named("m")),
	}
	got, err := Apply(incidents, Criteria{Status: "all", Sort: "severity"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if want := []string{"h", "m", "l", "u"}; len(ids(got)) != len(want) {
		t.Fatalf("order = %v, want %v", ids(got), want)
	} else {
		for k, w := range want {
			if ids(got)[k] != w {
				t.Fatalf("order = %v, want %v", ids(got), want)
			}
		}
	}
}

func TestSortPKAscendingNullLast(t *testing.T) {
	incidents := []model.Incident{
		i(interval(flt(120), flt(130)), named("120")),
		i(named("nil")),
		i(interval(flt(100), flt(110)), named("100")),
		i(interval(nil, flt(105)), named("105-from-to-only")),
	}
	got, err := Apply(incidents, Criteria{Status: "all", Sort: "pk"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := []string{"100", "105-from-to-only", "120", "nil"}
	gotIDs := ids(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("order = %v, want %v", gotIDs, want)
	}
	for k, w := range want {
		if gotIDs[k] != w {
			t.Fatalf("order = %v, want %v", gotIDs, want)
		}
	}
}

func TestSortStartEarliestFirst(t *testing.T) {
	incidents := []model.Incident{
		i(startAt("2026-10-28T12:00:00Z"), named("noon")),
		i(startAt("2026-10-28T06:00:00Z"), named("dawn")),
		i(startAt("2026-10-28T09:00:00Z"), named("morning")),
	}
	got, err := Apply(incidents, Criteria{Status: "all", Sort: "start"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := []string{"dawn", "morning", "noon"}
	gotIDs := ids(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("order = %v, want %v", gotIDs, want)
	}
	for k, w := range want {
		if gotIDs[k] != w {
			t.Fatalf("order = %v, want %v", gotIDs, want)
		}
	}
}

func TestSortTiesKeepInputOrder(t *testing.T) {
	incidents := []model.Incident{
		i(severity("high"), named("first")),
		i(severity("high"), named("second")),
	}
	got, err := Apply(incidents, Criteria{Status: "all", Sort: "severity"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if want := []string{"first", "second"}; len(ids(got)) != 2 || ids(got)[0] != want[0] || ids(got)[1] != want[1] {
		t.Errorf("ties reordered: %v, want %v", ids(got), want)
	}
}

func TestSortUnknownValueIsUsageError(t *testing.T) {
	_, err := Apply([]model.Incident{i()}, Criteria{Sort: "distance"})
	assertUsage(t, err)
}

// --- limit ---------------------------------------------------------------

func TestLimitAppliesAfterSorting(t *testing.T) {
	incidents := []model.Incident{
		i(severity("unknown"), named("u")),
		i(severity("low"), named("l")),
		i(severity("high"), named("h")),
		i(severity("medium"), named("m")),
	}
	got, err := Apply(incidents, Criteria{Status: "all", Sort: "severity", Limit: 2})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if want := []string{"h", "m"}; len(ids(got)) != 2 || ids(got)[0] != want[0] || ids(got)[1] != want[1] {
		t.Errorf("limited order = %v, want %v", ids(got), want)
	}
}

func TestLimitZeroReturnsEverything(t *testing.T) {
	incidents := []model.Incident{i(named("a")), i(named("b")), i(named("c"))}
	got, err := Apply(incidents, Criteria{Status: "all", Limit: 0})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Incidents) != 3 {
		t.Errorf("Limit 0 returned %d incidents, want 3", len(got.Incidents))
	}
}

// --- direction: incident "both" ------------------------------------------

// docs/feeds.md section 4.4: an incident with direction "both" affects both
// directions of travel, so it answers a positive or a negative question too.
func TestDirectionBothIncidentMatchesDirectionalFilters(t *testing.T) {
	incidents := []model.Incident{i(dir(strPtr("both")), named("both-dir"))}
	tests := []struct {
		name      string
		direction string
		wantMatch bool
	}{
		{name: "positive filter matches a both incident", direction: "positive", wantMatch: true},
		{name: "negative filter matches a both incident", direction: "negative", wantMatch: true},
		{name: "no filter matches a both incident", direction: "", wantMatch: true},
		{name: "both filter matches a both incident", direction: "both", wantMatch: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(incidents, Criteria{Direction: tt.direction, Status: "all"})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if match := len(got.Incidents) == 1; match != tt.wantMatch {
				t.Errorf("matched = %v, want %v (matches: %v)", match, tt.wantMatch, ids(got))
			}
		})
	}
}

// --- PK: incident with both bounds nil -----------------------------------

// A point location serialises one bound as null; a PK with BOTH bounds null
// says the feed gave no bounds at all, so it claims no knowledge and never
// matches a range, exactly like a nil PK.
func TestPKBothBoundsNilNeverMatches(t *testing.T) {
	incidents := []model.Incident{i(interval(nil, nil), named("no-bounds"))}
	got, err := Apply(incidents, Criteria{Roads: []string{"A-7"}, PK: &Range{From: flt(100), To: flt(140)}, Status: "all"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Incidents) != 0 {
		t.Errorf("incident with both PK bounds nil matched --pk 100-140: %v", ids(got))
	}
}

// --- unlocated composition ----------------------------------------------

// A row only counts as unlocated after it has passed every other filter and
// is then rejected for having no province: meta.unlocated means "rows that
// would have matched but could not be placed".
func TestUnlocatedCountsOnlyRowsThatPassOtherFilters(t *testing.T) {
	incidents := []model.Incident{
		i(severity("high"), province(""), named("high-noprov")),
		i(severity("low"), province(""), named("low-noprov")),
		i(severity("low"), province("Madrid"), named("low-madrid")),
	}
	got, err := Apply(incidents, Criteria{Severities: []string{"low"}, Provinces: []string{"Madrid"}, Status: "all"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if want := []string{"low-madrid"}; len(ids(got)) != 1 || ids(got)[0] != want[0] {
		t.Errorf("matches = %v, want %v", ids(got), want)
	}
	if got.Unlocated != 1 {
		t.Errorf("Unlocated = %d, want 1 (only the no-province row that passed the severity filter)", got.Unlocated)
	}
}

// --- limit ---------------------------------------------------------------

func TestNegativeLimitIsUsageError(t *testing.T) {
	_, err := Apply([]model.Incident{i()}, Criteria{Limit: -1})
	assertUsage(t, err)
}

// --- sort: unmapped severity --------------------------------------------

// An empty severity ranks last like unknown: the "unknown last" guarantee
// holds for any input, not only for values the model produces.
func TestSortSeverityEmptySeveritySortsLast(t *testing.T) {
	incidents := []model.Incident{
		i(severity("low"), named("l")),
		i(severity("unknown"), named("u")),
		i(severity(""), named("empty")),
	}
	got, err := Apply(incidents, Criteria{Status: "all", Sort: "severity"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := []string{"l", "u", "empty"}
	gotIDs := ids(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("order = %v, want %v", gotIDs, want)
	}
	for k, w := range want {
		if gotIDs[k] != w {
			t.Fatalf("order = %v, want %v", gotIDs, want)
		}
	}
}

// --- purity --------------------------------------------------------------

func TestApplyDoesNotMutateInput(t *testing.T) {
	incidents := []model.Incident{
		i(severity("unknown"), named("u")),
		i(severity("high"), named("h")),
		i(severity("low"), named("l")),
	}
	before := make([]model.Incident, len(incidents))
	copy(before, incidents)
	if _, err := Apply(incidents, Criteria{Status: "all", Sort: "severity", Limit: 1, Provinces: []string{"Madrid"}}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for k := range before {
		if incidents[k].ID != before[k].ID || incidents[k].Severity != before[k].Severity {
			t.Fatalf("input mutated at index %d", k)
		}
	}
	if len(incidents) != 3 {
		t.Fatalf("input slice length changed: %d", len(incidents))
	}
}
