package geo

import (
	"errors"
	"math"
	"testing"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
)

func coords(lat, lon float64) model.Coordinates {
	return model.Coordinates{Latitude: lat, Longitude: lon}
}

func flt(v float64) *float64 { return &v }

// --- haversine -----------------------------------------------------------

func TestHaversineKnownDistances(t *testing.T) {
	tests := []struct {
		name      string
		a, b      model.Coordinates
		want      float64
		tolerance float64
	}{
		{
			name:      "Alicante-Elche is about 20 km",
			a:         coords(38.3452, -0.4810),
			b:         coords(38.2622, -0.7079),
			want:      20,
			tolerance: 3,
		},
		{
			name:      "Madrid-Barcelona is about 505 km",
			a:         coords(40.4168, -3.7038),
			b:         coords(41.3874, 2.1686),
			want:      505,
			tolerance: 6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Haversine(tt.a, tt.b)
			if math.Abs(got-tt.want) > tt.tolerance {
				t.Errorf("Haversine() = %.1f km, want %.1f km (+/- %.1f)", got, tt.want, tt.tolerance)
			}
		})
	}
}

func TestHaversineSymmetricAndZero(t *testing.T) {
	a := coords(38.3452, -0.4810)
	b := coords(41.3874, 2.1686)
	if ab, ba := Haversine(a, b), Haversine(b, a); ab != ba {
		t.Errorf("Haversine(a,b) = %v, Haversine(b,a) = %v, want equal", ab, ba)
	}
	if zero := Haversine(a, a); zero != 0 {
		t.Errorf("Haversine(a,a) = %v, want 0", zero)
	}
}

// --- radius filter -------------------------------------------------------

func incidentAt(id string, c model.Coordinates, withStart bool) model.Incident {
	inc := model.Incident{
		ID:        id,
		StartTime: "2026-10-28T06:00:00Z",
	}
	if withStart {
		inc.Location.Start = &model.Coordinates{Latitude: c.Latitude, Longitude: c.Longitude}
	}
	return inc
}

func TestFilterRadiusKeepsStartPointsInside(t *testing.T) {
	alicante := coords(38.3452, -0.4810)
	elche := coords(38.2622, -0.7079)
	barcelona := coords(41.3874, 2.1686)
	incidents := []model.Incident{
		incidentAt("elche", elche, true),
		incidentAt("barcelona", barcelona, true),
		incidentAt("no-coords", alicante, false),
	}
	kept, unlocated := FilterRadius(incidents, alicante, 25)
	if len(kept) != 1 || kept[0].ID != "elche" {
		t.Errorf("kept = %v, want only elche (start point within 25 km)", ids(kept))
	}
	if unlocated != 1 {
		t.Errorf("unlocated = %d, want 1 (the incident without coordinates)", unlocated)
	}
}

func TestFilterRadiusBoundaryIsInclusive(t *testing.T) {
	centre := coords(38.3452, -0.4810)
	point := coords(38.2622, -0.7079)
	d := Haversine(centre, point)
	incidents := []model.Incident{incidentAt("edge", point, true)}

	if kept, _ := FilterRadius(incidents, centre, d); len(kept) != 1 {
		t.Errorf("incident exactly at the radius was dropped (radius %v km)", d)
	}
	if kept, _ := FilterRadius(incidents, centre, d-1e-6); len(kept) != 0 {
		t.Errorf("incident just past the radius was kept (radius %v km)", d-1e-6)
	}
}

func TestFilterRadiusDoesNotMutateInput(t *testing.T) {
	centre := coords(0, 0)
	incidents := []model.Incident{
		incidentAt("far", coords(10, 10), true),
		incidentAt("near", coords(0.01, 0.01), true),
	}
	if _, _ = FilterRadius(incidents, centre, 50); len(incidents) != 2 {
		t.Fatalf("input mutated: %d incidents left", len(incidents))
	}
	if incidents[0].ID != "far" || incidents[1].ID != "near" {
		t.Fatal("input order changed")
	}
}

func ids(incidents []model.Incident) []string {
	out := make([]string, 0, len(incidents))
	for _, inc := range incidents {
		out = append(out, inc.ID)
	}
	return out
}

// --- place resolution ----------------------------------------------------

func places() map[string]string {
	return map[string]string{"casa": "38.3452,-0.4810"}
}

func assertUsage(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("got nil error, want usage error")
	}
	var target *errs.Error
	if !errors.As(err, &target) {
		t.Fatalf("error = %v, want *errs.Error", err)
	}
	if target.ExitCode != errs.ExitUsage {
		t.Errorf("exit code = %d, want %d (usage)", target.ExitCode, errs.ExitUsage)
	}
}

func TestResolvePlaceKnown(t *testing.T) {
	got, err := ResolvePlace("casa", places())
	if err != nil {
		t.Fatalf("ResolvePlace() error = %v", err)
	}
	if got != (model.Coordinates{Latitude: 38.3452, Longitude: -0.4810}) {
		t.Errorf("ResolvePlace() = %v, want 38.3452,-0.4810", got)
	}
}

func TestResolvePlaceToleratesSpaces(t *testing.T) {
	got, err := ResolvePlace("casa", map[string]string{"casa": " 38.3452 , -0.4810 "})
	if err != nil {
		t.Fatalf("ResolvePlace() error = %v", err)
	}
	if got.Latitude != 38.3452 || got.Longitude != -0.4810 {
		t.Errorf("ResolvePlace() = %v, want 38.3452,-0.4810", got)
	}
}

func TestResolvePlaceUnknownIsUsageError(t *testing.T) {
	_, err := ResolvePlace("desconocido", places())
	assertUsage(t, err)
}

func TestResolvePlaceMalformedIsUsageError(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing longitude", value: "38.3452"},
		{name: "not a number", value: "north,-0.4810"},
		{name: "latitude out of range", value: "95.0,-0.4810"},
		{name: "longitude out of range", value: "38.3452,-200.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolvePlace("casa", map[string]string{"casa": tt.value})
			assertUsage(t, err)
		})
	}
}

// --- target combination rules --------------------------------------------

// A negative --radius must be rejected even when no target was given:
// silently ignoring the flag and running without a geo filter is worse than
// a usage error.
func TestResolveTargetNegativeRadiusWithoutTargetIsUsageError(t *testing.T) {
	got, err := ResolveTarget("", nil, nil, -5, places())
	assertUsage(t, err)
	if got != nil {
		t.Errorf("ResolveTarget() = %v, want nil", got)
	}
}

// Radius 0 with no target keeps meaning "no geo filter".
func TestResolveTargetZeroRadiusWithoutTargetMeansNoGeoFilter(t *testing.T) {
	got, err := ResolveTarget("", nil, nil, 0, places())
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if got != nil {
		t.Errorf("ResolveTarget() = %v, want nil (no geo filter)", got)
	}
}

func TestResolveTarget(t *testing.T) {
	tests := []struct {
		name     string
		near     string
		lat, lon *float64
		radiusKm float64
		want     *model.Coordinates
		wantErr  bool
	}{
		{
			name:     "near with radius resolves the saved place",
			near:     "casa",
			radiusKm: 15,
			want:     &model.Coordinates{Latitude: 38.3452, Longitude: -0.4810},
		},
		{
			name:     "lat and lon with radius are allowed without near",
			lat:      flt(38.3452),
			lon:      flt(-0.4810),
			radiusKm: 15,
			want:     &model.Coordinates{Latitude: 38.3452, Longitude: -0.4810},
		},
		{
			name:     "near together with lat and lon is a usage error",
			near:     "casa",
			lat:      flt(38.3452),
			lon:      flt(-0.4810),
			radiusKm: 15,
			wantErr:  true,
		},
		{
			name:     "near together with lat only is a usage error",
			near:     "casa",
			lat:      flt(38.3452),
			radiusKm: 15,
			wantErr:  true,
		},
		{
			name:     "no target with a radius is a usage error",
			radiusKm: 15,
			wantErr:  true,
		},
		{
			name:     "latitude only is a usage error",
			lat:      flt(38.3452),
			radiusKm: 15,
			wantErr:  true,
		},
		{
			name:    "target without a radius is a usage error",
			near:    "casa",
			wantErr: true,
		},
		{
			name:     "unknown place is a usage error",
			near:     "nope",
			radiusKm: 15,
			wantErr:  true,
		},
		{
			name:    "no target and no radius means no geo filter",
			want:    nil,
			wantErr: false,
		},
		{
			name:     "negative radius is a usage error",
			near:     "casa",
			radiusKm: -1,
			wantErr:  true,
		},
		{
			name:     "out of range coordinates are a usage error",
			lat:      flt(95),
			lon:      flt(-0.4810),
			radiusKm: 15,
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTarget(tt.near, tt.lat, tt.lon, tt.radiusKm, places())
			if tt.wantErr {
				assertUsage(t, err)
				return
			}
			if err != nil {
				t.Fatalf("ResolveTarget() error = %v", err)
			}
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("ResolveTarget() = %v, want nil", got)
			case tt.want != nil && (got == nil || *got != *tt.want):
				t.Errorf("ResolveTarget() = %v, want %v", got, tt.want)
			}
		})
	}
}
