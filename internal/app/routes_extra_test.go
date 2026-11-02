package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/model"
)

// A segment radar publishes its range unordered (docs/feeds.md §6: ini 2650 m,
// fin 1400 m). Sorting the interval before the overlap test is what keeps
// such a radar visible on its own segment instead of silently missing.
func TestRadarOnSegmentMatchesInvertedPK(t *testing.T) {
	radar := model.Radar{
		Road: "N-433",
		PK:   &model.PK{From: floatPtr(2.65), To: floatPtr(1.4)},
	}

	if !radarOnSegment(radar, RouteSegmentView{Road: "N-433", From: 1, To: 2}) {
		t.Error("a radar with from > to did not match the segment that contains its 1.4-2.65 range")
	}
	if !radarOnSegment(radar, RouteSegmentView{Road: "N-433", From: 2, To: 3}) {
		t.Error("a radar with from > to did not match the segment that overlaps its upper bound")
	}
	if radarOnSegment(radar, RouteSegmentView{Road: "N-433", From: 5, To: 9}) {
		t.Error("a radar matched a segment that does not overlap its range")
	}
	if radarOnSegment(radar, RouteSegmentView{Road: "A-7", From: 1, To: 9}) {
		t.Error("a radar matched a different road")
	}
	if radarOnSegment(model.Radar{Road: "N-433"}, RouteSegmentView{Road: "N-433", From: 0, To: 9}) {
		t.Error("a radar without a PK matched; an unranged radar is not on the segment")
	}
}

// A panel publishes one kilometre point, not a range, so the bounds are
// inclusive on both ends and nothing outside them matches.
func TestSignOnSegmentUsesASinglePoint(t *testing.T) {
	sign := model.Sign{Road: "Z-40", PK: floatPtr(12.5)}

	if !signOnSegment(sign, RouteSegmentView{Road: "Z-40", From: 12, To: 13}) {
		t.Error("a panel inside the segment did not match")
	}
	if !signOnSegment(sign, RouteSegmentView{Road: "Z-40", From: 12.5, To: 13}) {
		t.Error("a panel exactly on the lower bound did not match")
	}
	if signOnSegment(sign, RouteSegmentView{Road: "Z-40", From: 13, To: 20}) {
		t.Error("a panel past the segment end matched")
	}
	if signOnSegment(model.Sign{Road: "Z-40"}, RouteSegmentView{Road: "Z-40", From: 0, To: 99}) {
		t.Error("a panel without a PK matched")
	}
}

// The extra feeds are opt-in, but their keys always travel: a consumer must
// see the same shape whether or not they were requested, the same reason
// speed_limit exists as null on a radar.
func TestRouteCheckSegmentKeepsExtraFeedKeysWhenNotRequested(t *testing.T) {
	data, err := json.Marshal(RouteCheckSegment{Road: "A-7", Incidents: []model.Incident{}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	encoded := string(data)
	for _, key := range []string{`"signs":null`, `"radars":null`} {
		if !strings.Contains(encoded, key) {
			t.Errorf("segment JSON = %s, want %s", encoded, key)
		}
	}
}
