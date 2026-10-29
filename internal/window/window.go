// Package window parses time-window expressions for `watch --window`
// (WATCH-01a, OLA-3 spec S4) and decides whether an instant falls inside a
// window. All windows are evaluated in Europe/Madrid, whatever location the
// caller's time carries.
//
// # Spec format
//
// A window is "<days> <start>-<end>", for example "lun-vie 07:00-09:00".
//
// Days are case-insensitive, in Spanish or English, accented or not
// (mié/mie, sábado/sabado), as three-letter abbreviations or full names.
// They are a single day ("lun"), an inclusive range ("lun-vie", which
// wraps when the start is after the end: "vie-lun" is Friday, Saturday,
// Sunday and Monday), or a comma list mixing both ("lun,mié-vie").
// Anything else is a usage error (exit 2) naming what was received.
//
// Times are HH:MM, 24-hour clock, exactly two digits per field
// ("7:00" is rejected). start == end is a usage error: an empty window
// that never fires is a trap.
//
// # Conventions pinned by tests
//
//   - End-exclusive: a window covers [start, end). 09:00 does NOT match
//     "07:00-09:00"; 08:59:59 does. Minutes are the finest granularity
//     compared, so 08:59:59.9 still matches.
//
//   - A window whose start is greater than its end ("22:00-02:00") crosses
//     midnight and belongs to the day it starts on: "vie 22:00-02:00"
//     covers Friday 22:00 through Saturday 02:00 (exclusive), and its tail
//     only fires when the starting day is declared. Monday 23:00 never
//     matches it.
//
//   - DST: matching compares Madrid wall-clock readings (hour/minute of the
//     local time), never constructed wall times. On the spring-forward day
//     (2026-03-29, 02:00→03:00) the wall times 02:00-02:59 do not exist, so
//     a "02:30-03:30" window simply covers the real instants 03:00-03:29:59
//     of that day; nothing panics. On the fall-back day (2026-10-25,
//     03:00→02:00) an ambiguous wall time such as 02:30 occurs twice and
//     matches in both occurrences.
package window

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
)

// weekdayIndex is the day value used internally: Monday=0 through Sunday=6,
// because window ranges are easier to wrap in that order.
type weekdayIndex = int

// dayNames lists, for every weekday in Monday=0 order, the accepted Spanish
// and English spellings (already covering accented and unaccented forms).
// The first entry of each row is the canonical Spanish abbreviation used by
// Window.String.
var dayNames = [7][]string{
	{"lun", "lunes", "mon", "monday"},
	{"mar", "martes", "tue", "tuesday"},
	{"mié", "mie", "miércoles", "miercoles", "wed", "wednesday"},
	{"jue", "jueves", "thu", "thursday"},
	{"vie", "viernes", "fri", "friday"},
	{"sáb", "sab", "sábado", "sabado", "sat", "saturday"},
	{"dom", "domingo", "sun", "sunday"},
}

// dayLookup maps every accepted spelling to its weekdayIndex, built once.
var dayLookup = func() map[string]weekdayIndex {
	lookup := make(map[string]weekdayIndex, 7*4)
	for index, names := range dayNames {
		for _, name := range names {
			lookup[name] = index
		}
	}
	return lookup
}()

// Window is a parsed time window: the days it starts on (sorted Monday
// first, unique) and its start and end as minutes since Madrid midnight.
type Window struct {
	// Days are the weekdays the window starts on, sorted Monday first
	// and with no duplicates.
	Days []time.Weekday
	// StartMinute is the window start in minutes since Madrid midnight,
	// inclusive.
	StartMinute int
	// EndMinute is the window end in minutes since Madrid midnight,
	// exclusive. Smaller than StartMinute when the window crosses midnight.
	EndMinute int
}

// daySet converts Days into a lookup by internal index.
func (w Window) daySet() [7]bool {
	var set [7]bool
	for _, day := range w.Days {
		set[(int(day)+6)%7] = true
	}
	return set
}

// CrossesMidnight reports whether the window spans midnight (start greater
// than end, for example 22:00-02:00).
func (w Window) CrossesMidnight() bool {
	return w.EndMinute < w.StartMinute
}

// Matches reports whether the instant t falls inside the window. The
// comparison uses Europe/Madrid wall-clock readings regardless of the
// location t carries. See the package documentation for the end-exclusive,
// midnight-crossing and DST conventions.
func (w Window) Matches(t time.Time) bool {
	if len(w.Days) == 0 {
		return false
	}
	local := t.In(Madrid())
	minute := local.Hour()*60 + local.Minute()
	day := (int(local.Weekday()) + 6) % 7 // Monday=0
	set := w.daySet()

	if !w.CrossesMidnight() {
		return set[day] && minute >= w.StartMinute && minute < w.EndMinute
	}
	// The window belongs to the day it starts on: its tail (00:00 to end)
	// fires only on the day after a declared start day.
	if set[day] && minute >= w.StartMinute {
		return true
	}
	previous := (day + 6) % 7
	return set[previous] && minute < w.EndMinute
}

// String returns a canonical, human-readable form for `watch list`: Spanish
// day abbreviations sorted Monday first, consecutive runs collapsed into
// ranges, and zero-padded HH:MM times. For example "lun,mié-vie 07:00-09:00".
//
// A window with no days — reachable only from a zero value, never from Parse —
// renders as an explicit empty marker: it has no days, so claiming a time
// range for it would be a lie that `watch list` would repeat.
func (w Window) String() string {
	set := w.daySet()
	if len(w.Days) == 0 {
		return "(sin días)"
	}
	var parts []string
	for start := 0; start < 7; start++ {
		if !set[start] {
			continue
		}
		end := start
		for end+1 < 7 && set[end+1] {
			end++
		}
		switch {
		case start == end:
			parts = append(parts, dayNames[start][0])
		default:
			parts = append(parts, dayNames[start][0]+"-"+dayNames[end][0])
		}
		start = end
	}
	return fmt.Sprintf("%s %02d:%02d-%02d:%02d",
		strings.Join(parts, ","), w.StartMinute/60, w.StartMinute%60, w.EndMinute/60, w.EndMinute%60)
}

// Parse parses one window spec "<days> <start>-<end>" (for example
// "lun-vie 07:00-09:00"), or returns a usage error (exit 2) naming what was
// received. The returned Window is the zero value whenever an error is
// returned.
func Parse(spec string) (Window, error) {
	fields := strings.Fields(spec)
	if len(fields) != 2 {
		return Window{}, errs.NewUsage(fmt.Sprintf(
			"la ventana %q debe ser \"días HH:MM-HH:MM\", por ejemplo \"lun-vie 07:00-09:00\"", spec))
	}
	days, err := parseDays(fields[0], spec)
	if err != nil {
		return Window{}, err
	}
	start, end, err := parseTimes(fields[1], spec)
	if err != nil {
		return Window{}, err
	}
	if start == end {
		return Window{}, errs.NewUsage(fmt.Sprintf(
			"la ventana %q nunca se cumple: la hora de inicio y la de fin son iguales", spec))
	}
	return Window{Days: days, StartMinute: start, EndMinute: end}, nil
}

// ParseAll parses a list of window specs (repeated --window flags) and
// returns the first error found, with no partial result.
func ParseAll(specs []string) ([]Window, error) {
	windows := make([]Window, 0, len(specs))
	for _, spec := range specs {
		w, err := Parse(spec)
		if err != nil {
			return nil, err
		}
		windows = append(windows, w)
	}
	return windows, nil
}

// parseDays parses the day section of a spec: single days, inclusive
// wrapping ranges and comma lists.
func parseDays(dayPart, spec string) ([]time.Weekday, error) {
	var set [7]bool
	for _, token := range strings.Split(dayPart, ",") {
		if token == "" {
			return nil, emptyDayError(dayPart, spec)
		}
		if strings.Contains(token, "-") {
			sides := strings.Split(token, "-")
			if len(sides) != 2 || sides[0] == "" || sides[1] == "" {
				return nil, unknownDayError(token, spec)
			}
			from, err := resolveDay(sides[0], spec)
			if err != nil {
				return nil, err
			}
			to, err := resolveDay(sides[1], spec)
			if err != nil {
				return nil, err
			}
			for day := from; ; day = (day + 1) % 7 {
				set[day] = true
				if day == to {
					break
				}
			}
			continue
		}
		day, err := resolveDay(token, spec)
		if err != nil {
			return nil, err
		}
		set[day] = true
	}

	var days []time.Weekday
	for index := 0; index < 7; index++ {
		if set[index] {
			days = append(days, time.Weekday((index+1)%7))
		}
	}
	return days, nil
}

// resolveDay maps one day token (case-insensitive) to its weekdayIndex.
func resolveDay(token, spec string) (weekdayIndex, error) {
	day, ok := dayLookup[strings.ToLower(token)]
	if !ok {
		return 0, unknownDayError(token, spec)
	}
	return day, nil
}

// unknownDayError rejects a day token that no accepted spelling matches.
func unknownDayError(token, spec string) error {
	return errs.NewUsage(fmt.Sprintf(
		"día desconocido %q en la ventana %q: usa lun, mar, mié, jue, vie, sáb, dom (o mon-sun, con rango \"lun-vie\" y lista \"lun,mié-vie\")",
		token, spec))
}

// emptyDayError rejects an empty day token such as "lun,,mar" or "lun-vie,".
func emptyDayError(dayPart, spec string) error {
	return errs.NewUsage(fmt.Sprintf(
		"la lista de días %q de la ventana %q tiene un día vacío: usa \"lun\", \"lun-vie\" o \"lun,mié-vie\"",
		dayPart, spec))
}

// parseTimes parses the "HH:MM-HH:MM" section of a spec. Exactly one dash is
// allowed; anything else (including a trailing dash) is a usage error.
func parseTimes(timePart, spec string) (start, end int, err error) {
	parts := strings.Split(timePart, "-")
	if len(parts) != 2 {
		return 0, 0, errs.NewUsage(fmt.Sprintf(
			"la franja horaria %q de la ventana %q debe ser \"HH:MM-HH:MM\", por ejemplo \"07:00-09:00\"",
			timePart, spec))
	}
	start, err = parseClock(parts[0], spec)
	if err != nil {
		return 0, 0, err
	}
	end, err = parseClock(parts[1], spec)
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

// parseClock parses one "HH:MM" time: exactly two digits per field, 24-hour
// clock, minutes 00-59. "7:00", "25:00" and "07:60" are usage errors.
func parseClock(value, spec string) (int, error) {
	invalid := errs.NewUsage(fmt.Sprintf(
		"hora inválida %q en la ventana %q: usa HH:MM de 24 horas con dos dígitos, por ejemplo \"07:00\"",
		value, spec))
	if len(value) != 5 || value[2] != ':' ||
		!isDigit(value[0]) || !isDigit(value[1]) || !isDigit(value[3]) || !isDigit(value[4]) {
		return 0, invalid
	}
	hours := int(value[0]-'0')*10 + int(value[1]-'0')
	minutes := int(value[3]-'0')*10 + int(value[4]-'0')
	if hours > 23 || minutes > 59 {
		return 0, invalid
	}
	return hours*60 + minutes, nil
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// madridOnce caches the Europe/Madrid location so every window check in a
// process resolves it once.
var madridOnce = sync.OnceValue(func() *time.Location {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		return time.UTC
	}
	return loc
})

// Madrid returns Europe/Madrid, the timezone every window is evaluated in
// (spec S4). It mirrors madridLocation in internal/cli without importing it:
// the zone database is embedded in the binary (cmd/trafico imports
// time/tzdata), so this succeeds on machines with no zoneinfo; if it somehow
// does not, windows fall back to UTC and keep working, just anchored to UTC.
func Madrid() *time.Location {
	return madridOnce()
}
