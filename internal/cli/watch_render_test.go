package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
)

// The FORMAT-03 tests: the three watch commands still on the JSON fallback
// (watch check, watch run, watch service) get a real text renderer. Every
// test here asserts, through runCLIDefaultFormat, that the output is NOT the
// indented-JSON fallback (assertTextRender pins first-line-not-"{", no
// envelope key leak, attribution line last) and that the readable Spanish
// shape a person needs is actually there. The --json contract stays pinned
// byte for byte by the existing watch tests in watch_test.go.

// TestWatchCheckTextReportsEvents pins the human-readable default of
// `trafico watch check <name>` when the check finds changes: per event, the
// kind in Spanish and the incident it refers to — never a JSON array.
func TestWatchCheckTextReportsEvents(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	// The baseline run gives the watch a previous state, so a later check can
	// actually detect a change.
	if code, _, stderr := runCLI(t, "watch", "run"); code != errs.ExitSuccess {
		t.Fatalf("watch run (baseline) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	dropFeedCache(t)
	srv.setBody(swapIncidentID(t, "22365494", "9999999"))

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "check", "ruta")
	if code != errs.ExitSuccess {
		t.Fatalf("watch check exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "ruta") {
		t.Errorf("the text output must name the watch, got %.200s", out)
	}
	if !strings.Contains(out, "nueva") {
		t.Errorf("an incident.new event must read as nueva, got %.200s", out)
	}
	if !strings.Contains(out, "9999999") {
		t.Errorf("the event must name the incident it refers to, got %.200s", out)
	}
}

// TestWatchCheckTextNothingChanged pins the nothing-changed report: one line
// saying so, never an empty JSON array.
func TestWatchCheckTextNothingChanged(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if code, _, stderr := runCLI(t, "watch", "run"); code != errs.ExitSuccess {
		t.Fatalf("watch run (baseline) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "check", "ruta")
	if code != errs.ExitSuccess {
		t.Fatalf("watch check exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "sin novedades") {
		t.Errorf("an unchanged check must say so in one line, got %.200s", out)
	}
	if strings.Contains(out, "[]") {
		t.Errorf("an unchanged check must not print an empty array, got %.200s", out)
	}
}

// TestWatchCheckTextOutsideWindow pins the skipped report of the manual
// check (S4): the readable text says the watch is outside its window and
// nothing was checked.
func TestWatchCheckTextOutsideWindow(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	_, outWindow := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo", "--window", outWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "check", "ruta")
	if code != errs.ExitSuccess {
		t.Fatalf("watch check exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "ventana") {
		t.Errorf("a skipped check must explain the window gate, got %.200s", out)
	}
}

// TestWatchRunTextRendersLogLines pins the human-readable default of
// `trafico watch run`: one timestamped line per watch (it runs unattended on
// a timer, so the text must be useful in a log) plus one closing summary,
// with no repeated headers.
func TestWatchRunTextRendersLogLines(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, outWindow := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "activa", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add (activa) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if code, _, stderr := runCLI(t, "watch", "add", "nocturna", "route", "trabajo", "--window", outWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add (nocturna) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		t.Fatalf("watch run text = %q, want at least two watch lines and a summary", out)
	}
	// The first line is a log entry: a timestamp, then the watch name.
	fields := strings.Fields(lines[0])
	if len(fields) < 3 {
		t.Fatalf("the first watch line %q is not a timestamped log entry", lines[0])
	}
	if _, err := time.Parse(time.RFC3339, fields[0]); err != nil {
		t.Errorf("the first watch line must start with an RFC3339 timestamp, got %q: %v", lines[0], err)
	}
	if fields[1] != "activa:" {
		t.Errorf("the first watch line must name the watch, got %q", lines[0])
	}
	if !strings.Contains(out, "línea base registrada") {
		t.Errorf("a baseline run must say so, got %.200s", out)
	}
	if !strings.Contains(out, "nocturna: fuera de ventana") {
		t.Errorf("a window-skipped watch must be reported, got %.200s", out)
	}
	if !strings.Contains(out, "Resumen: 2 vigilancias") {
		t.Errorf("the closing summary must count the watches, got %.200s", out)
	}
}

// TestWatchRunTextSingularSummary pins the grammar of the closing summary
// when the cycle has exactly one watch: "1 vigilancia", never "1 vigilancias".
func TestWatchRunTextSingularSummary(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	_, outWindow := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "nocturna", "route", "trabajo", "--window", outWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "Resumen: 1 vigilancia,") {
		t.Errorf("the summary must use the singular for one watch, got %.200s", out)
	}
}

// TestWatchServiceTextLifecycle walks the scheduler lifecycle in the
// readable default: status of nothing installed says so and what to do,
// install and uninstall confirm in one line each, and status of an
// installed, active service reads plainly. The service seam (the fake
// runner of watch_test.go) keeps every scheduler away.
func TestWatchServiceTextLifecycle(t *testing.T) {
	setCLIEnv(t)
	runner := &fakeServiceRunner{}
	setServiceContext(t, runner)

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "service", "status")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service status (not installed) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "no instalado") {
		t.Errorf("the status of nothing installed must say so, got %.200s", out)
	}
	if !strings.Contains(out, "trafico watch service install") {
		t.Errorf("the status of nothing installed must say what to do, got %.200s", out)
	}

	code, stdout, stderr = runCLIDefaultFormat(t, "watch", "service", "install")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service install exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out = stdout.String()
	assertTextRender(t, out)
	if first := strings.Split(strings.TrimSpace(out), "\n")[0]; !strings.Contains(first, "instalado") {
		t.Errorf("the install confirmation line must say instalado, got %q", first)
	}
	if !strings.Contains(out, "trafico-watch.timer") {
		t.Errorf("the install text must list the written unit files, got %.200s", out)
	}

	code, stdout, stderr = runCLIDefaultFormat(t, "watch", "service", "status")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service status (installed) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out = stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "instalado") || !strings.Contains(out, "activo") {
		t.Errorf("the status of an installed active service must say both, got %.200s", out)
	}

	code, stdout, stderr = runCLIDefaultFormat(t, "watch", "service", "uninstall")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service uninstall exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out = stdout.String()
	assertTextRender(t, out)
	if first := strings.Split(strings.TrimSpace(out), "\n")[0]; !strings.Contains(first, "desinstalado") {
		t.Errorf("the uninstall confirmation line must say desinstalado, got %q", first)
	}
}

// TestWatchServiceTextNothingToUninstall pins the no-op uninstall wording:
// it succeeds and says there was nothing installed.
func TestWatchServiceTextNothingToUninstall(t *testing.T) {
	setCLIEnv(t)
	runner := &fakeServiceRunner{}
	setServiceContext(t, runner)

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "service", "uninstall")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service uninstall (nothing installed) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "desinstalado") || !strings.Contains(out, "nada instalado") {
		t.Errorf("the no-op uninstall must say there was nothing installed, got %.200s", out)
	}
}
