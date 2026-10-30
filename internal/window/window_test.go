package window_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/window"
)

// mad builds an instant from Madrid wall-clock readings. Used only for wall
// times that actually exist; the DST tests build UTC instants by hand because
// 02:30 on the spring-forward day does not exist in Madrid.
func mad(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, window.Madrid())
}

// utc builds an instant in UTC. The equivalent Madrid wall-clock reading is
// stated in each test name/comment (CET = UTC+1, CEST = UTC+2).
func utc(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, time.UTC)
}

// weekdaySet turns a parsed Window into a compact string like "lun,vie-dom"
// so expectations read the same way the spec does.
func weekdaySet(w window.Window) string {
	names := map[time.Weekday]string{
		time.Monday: "mon", time.Tuesday: "tue", time.Wednesday: "wed",
		time.Thursday: "thu", time.Friday: "fri", time.Saturday: "sat",
		time.Sunday: "sun",
	}
	parts := make([]string, 0, len(w.Days))
	for _, d := range w.Days {
		parts = append(parts, names[d])
	}
	return strings.Join(parts, ",")
}

func TestParseDays(t *testing.T) {
	tests := []struct {
		name    string
		dayPart string
		want    string
	}{
		{"spanish three-letter", "lun", "mon"},
		{"spanish full name", "lunes", "mon"},
		{"english three-letter", "mon", "mon"},
		{"english full name", "monday", "mon"},
		{"accented uppercase", "MIÉ", "wed"},
		{"unaccented", "MIE", "wed"},
		{"unaccented full name", "sabado", "sat"},
		{"accented full name", "SÁBADO", "sat"},
		{"simple range", "lun-vie", "mon,tue,wed,thu,fri"},
		{"english range", "mon-fri", "mon,tue,wed,thu,fri"},
		{"wrapping range", "vie-lun", "mon,fri,sat,sun"},
		{"wrapping range english", "sat-mon", "mon,sat,sun"},
		{"mixed list", "lun,mié-vie", "mon,wed,thu,fri"},
		{"duplicates collapse", "lun,lunes,monday", "mon"},
		{"range with equal ends", "dom-dom", "sun"},
		{"full week", "lun-dom", "mon,tue,wed,thu,fri,sat,sun"},
		{"weekend", "sáb-sun", "sat,sun"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := window.Parse(tt.dayPart + " 07:00-09:00")
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.dayPart+" 07:00-09:00", err)
			}
			if got := weekdaySet(w); got != tt.want {
				t.Errorf("days of %q = %q, want %q", tt.dayPart, got, tt.want)
			}
		})
	}
}

func TestParseTimes(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		wantStart int
		wantEnd   int
	}{
		{"basic", "lun 07:00-09:00", 7 * 60, 9 * 60},
		{"midnight start", "lun 00:00-06:30", 0, 6*60 + 30},
		{"late end", "lun 21:15-23:59", 21*60 + 15, 23*60 + 59},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := window.Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.spec, err)
			}
			if w.StartMinute != tt.wantStart || w.EndMinute != tt.wantEnd {
				t.Errorf("Parse(%q) = start %d end %d, want start %d end %d",
					tt.spec, w.StartMinute, w.EndMinute, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestParseMalformed pins that every malformed spec is a usage error
// (errs.NewUsage, exit code 2) and never a panic or a silently accepted spec.
func TestParseMalformed(t *testing.T) {
	specs := []string{
		"",
		"   ",
		"lun",
		"lun-vie",
		"lun 7:00-9:00",
		"lun 7:00-09:00",
		"lun 07:0-09:00",
		"lun 25:00-26:00",
		"lun 24:00-25:00",
		"lun 07:60-08:00",
		"lun 07:00-07:00",
		"xyz 07:00-09:00",
		"lun-vie, 07:00-09:00",
		"lun,vie-xyz 07:00-09:00",
		"lun- 07:00-09:00",
		"lun 07:00",
		"lun 07:00-",
		"lun -09:00",
		"lun 07:00-09:00-11:00",
		"lun 07:00-09:00 extra",
		"lun-vie 07:00-09:00 17:00-19:00",
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			w, err := window.Parse(spec)
			if err == nil {
				t.Fatalf("Parse(%q) = %+v, want usage error", spec, w)
			}
			if len(w.Days) != 0 || w.StartMinute != 0 || w.EndMinute != 0 {
				t.Errorf("Parse(%q) returned a non-zero Window alongside the error", spec)
			}
			var target *errs.Error
			if !errors.As(err, &target) {
				t.Fatalf("Parse(%q) error is %T (%v), want *errs.Error", spec, err, err)
			}
			if target.ExitCode != errs.ExitUsage || target.Type != "usage" {
				t.Errorf("Parse(%q) error = type %q exit %d, want usage/2", spec, target.Type, target.ExitCode)
			}
		})
	}
}

func TestParseUnknownDayNamesWhatItReceived(t *testing.T) {
	_, err := window.Parse("xyz 07:00-09:00")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `"xyz"`) {
		t.Errorf("error message %q does not name the received day %q", err.Error(), "xyz")
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name string
		spec string
		at   time.Time
		want bool
	}{
		// End-exclusive convention: [07:00, 09:00) in Madrid wall clock.
		{"minute before start does not match", "lun 07:00-09:00", mad(2026, 3, 2, 6, 59, 59), false},
		{"exact start matches", "lun 07:00-09:00", mad(2026, 3, 2, 7, 0, 0), true},
		{"last second before end matches", "lun 07:00-09:00", mad(2026, 3, 2, 8, 59, 59), true},
		{"exact end does not match (end exclusive)", "lun 07:00-09:00", mad(2026, 3, 2, 9, 0, 0), false},
		{"other weekday does not match", "lun 07:00-09:00", mad(2026, 3, 7, 8, 0, 0), false},

		// Mixed day list.
		{"listed single day matches", "lun,mié-vie 07:00-09:00", mad(2026, 3, 2, 8, 0, 0), true},
		{"unlisted day does not match", "lun,mié-vie 07:00-09:00", mad(2026, 3, 3, 8, 0, 0), false},
		{"first day of range matches", "lun,mié-vie 07:00-09:00", mad(2026, 3, 4, 8, 0, 0), true},
		{"last day of range matches", "lun,mié-vie 07:00-09:00", mad(2026, 3, 6, 8, 0, 0), true},

		// Wrapping day range plus a midnight-crossing window: the window
		// belongs to the day it starts on, so its tail covers the next day.
		{"wrapping days: friday start matches", "vie-lun 22:00-02:00", mad(2026, 3, 6, 22, 0, 0), true},
		{"wrapping days: before start does not match", "vie-lun 22:00-02:00", mad(2026, 3, 6, 21, 59, 59), false},
		{"crossing window: late friday matches", "vie-lun 22:00-02:00", mad(2026, 3, 6, 23, 30, 0), true},
		{"crossing window: saturday tail matches", "vie-lun 22:00-02:00", mad(2026, 3, 7, 1, 0, 0), true},
		{"crossing window: exact end does not match", "vie-lun 22:00-02:00", mad(2026, 3, 7, 2, 0, 0), false},
		{"wrapping days: saturday evening matches", "vie-lun 22:00-02:00", mad(2026, 3, 7, 23, 0, 0), true},
		{"wrapping days: monday evening matches", "vie-lun 22:00-02:00", mad(2026, 3, 9, 23, 0, 0), true},
		{"wrapping days: wednesday does not match", "vie-lun 22:00-02:00", mad(2026, 3, 4, 23, 0, 0), false},
		{"crossing window: monday tail matches via sunday declared", "vie-lun 22:00-02:00", mad(2026, 3, 9, 1, 0, 0), true},
		{"wrapping days: sunday 03:00 is after the tail and before the start", "vie-lun 22:00-02:00", mad(2026, 3, 8, 3, 0, 0), false},

		// Plain midnight-crossing window on one day.
		{"vie 22:00-02:00: friday matches", "vie 22:00-02:00", mad(2026, 3, 6, 22, 0, 0), true},
		{"vie 22:00-02:00: saturday 01:00 matches", "vie 22:00-02:00", mad(2026, 3, 7, 1, 0, 0), true},
		{"vie 22:00-02:00: monday 23:00 does not match", "vie 22:00-02:00", mad(2026, 3, 9, 23, 0, 0), false},
		{"vie 22:00-02:00: friday 02:00 has no tail", "vie 22:00-02:00", mad(2026, 3, 6, 2, 0, 0), false},
		{"vie 22:00-02:00: thursday evening does not match", "vie 22:00-02:00", mad(2026, 3, 5, 23, 30, 0), false},
		{"vie 22:00-02:00: sunday 01:00 does not match", "vie 22:00-02:00", mad(2026, 3, 8, 1, 0, 0), false},

		// A UTC instant is matched against Madrid local time, not the UTC
		// clock. 2026-07-06 05:00 UTC is Monday 07:00 CEST (UTC+2).
		{"utc instant matched in madrid: inside", "lun 07:00-09:00", utc(2026, 7, 6, 5, 0, 0), true},
		{"utc instant matched in madrid: utc clock would match, madrid does not", "lun 04:00-06:00", utc(2026, 7, 6, 5, 0, 0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := window.Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.spec, err)
			}
			if got := w.Matches(tt.at); got != tt.want {
				t.Errorf("Parse(%q).Matches(%v) = %v, want %v", tt.spec, tt.at, got, tt.want)
			}
		})
	}
}

// TestMatchesDSTSpringForward pins the DST rule: matching compares Madrid
// wall-clock readings, so wall times that do not exist (02:00-02:59 on
// 2026-03-29, when clocks jump 02:00→03:00) never occur and the window
// 02:30-03:30 only covers the real instants 03:00-03:29:59 of that day.
func TestMatchesDSTSpringForward(t *testing.T) {
	w, err := window.Parse("dom 02:30-03:30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"01:00 CET (00:00 UTC) does not match", utc(2026, 3, 29, 0, 0, 0), false},
		{"01:59:59 CET (00:59:59 UTC) does not match", utc(2026, 3, 29, 0, 59, 59), false},
		{"03:00 CEST (01:00 UTC) matches", utc(2026, 3, 29, 1, 0, 0), true},
		{"03:29:59 CEST (01:29:59 UTC) matches", utc(2026, 3, 29, 1, 29, 59), true},
		{"03:30 CEST (01:30 UTC) does not match (end exclusive)", utc(2026, 3, 29, 1, 30, 0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := w.Matches(tt.at); got != tt.want {
				t.Errorf("Matches(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

// TestMatchesDSTFallBack pins that an ambiguous wall time (02:30 occurs twice
// on 2026-10-25, when clocks fall 03:00→02:00) matches in both occurrences.
func TestMatchesDSTFallBack(t *testing.T) {
	w, err := window.Parse("dom 02:30-03:30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"first 02:30 CEST (00:30 UTC) matches", utc(2026, 10, 25, 0, 30, 0), true},
		{"02:00 CET boundary (01:00 UTC) does not match", utc(2026, 10, 25, 1, 0, 0), false},
		{"second 02:30 CET (01:30 UTC) matches too", utc(2026, 10, 25, 1, 30, 0), true},
		{"03:30 CET (02:30 UTC) does not match (end exclusive)", utc(2026, 10, 25, 2, 30, 0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := w.Matches(tt.at); got != tt.want {
				t.Errorf("Matches(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{"lun-vie 07:00-09:00", "lun-vie 07:00-09:00"},
		{"monday 07:00-09:00", "lun 07:00-09:00"},
		{"LUN,MIE-VIERNES 07:00-09:00", "lun,mié-vie 07:00-09:00"},
		{"vie-lun 22:00-02:00", "lun,vie-dom 22:00-02:00"},
		{"sabado 00:00-23:59", "sáb 00:00-23:59"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			w, err := window.Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.spec, err)
			}
			if got := w.String(); got != tt.want {
				t.Errorf("Parse(%q).String() = %q, want %q", tt.spec, got, tt.want)
			}
		})
	}
}

func TestCrossesMidnight(t *testing.T) {
	same, err := window.Parse("lun 07:00-09:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if same.CrossesMidnight() {
		t.Error("07:00-09:00 should not report crossing midnight")
	}
	crossing, err := window.Parse("vie 22:00-02:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !crossing.CrossesMidnight() {
		t.Error("22:00-02:00 should report crossing midnight")
	}
}

func TestParseAll(t *testing.T) {
	t.Run("valid specs", func(t *testing.T) {
		windows, err := window.ParseAll([]string{"lun-vie 07:00-09:00", "sáb 10:00-12:00"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(windows) != 2 {
			t.Fatalf("len(windows) = %d, want 2", len(windows))
		}
		if windows[0].String() != "lun-vie 07:00-09:00" || windows[1].String() != "sáb 10:00-12:00" {
			t.Errorf("ParseAll windows = %q, %q", windows[0].String(), windows[1].String())
		}
	})
	t.Run("any bad spec fails the batch", func(t *testing.T) {
		windows, err := window.ParseAll([]string{"lun-vie 07:00-09:00", "bogus"})
		if err == nil {
			t.Fatal("expected an error")
		}
		if windows != nil {
			t.Errorf("ParseAll returned %v alongside the error, want nil", windows)
		}
		var target *errs.Error
		if !errors.As(err, &target) || target.ExitCode != errs.ExitUsage {
			t.Errorf("error = %v, want usage (exit 2)", err)
		}
	})
	t.Run("empty input yields no windows", func(t *testing.T) {
		windows, err := window.ParseAll(nil)
		if err != nil || len(windows) != 0 {
			t.Errorf("ParseAll(nil) = %v, %v, want empty, nil", windows, err)
		}
	})
}

func TestMadridLocation(t *testing.T) {
	loc := window.Madrid()
	if loc == nil {
		t.Fatal("Madrid() returned nil")
	}
	// Europe/Madrid is CET (UTC+1) in winter and CEST (UTC+2) in summer.
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).In(loc)
	if offset := winter.Format("-07:00"); offset != "+01:00" {
		t.Errorf("Madrid winter offset = %s, want +01:00", offset)
	}
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC).In(loc)
	if offset := summer.Format("-07:00"); offset != "+02:00" {
		t.Errorf("Madrid summer offset = %s, want +02:00", offset)
	}
}
