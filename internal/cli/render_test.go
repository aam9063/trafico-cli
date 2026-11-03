package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/errs"
)

// assertTextRender pins the two FORMAT-02 rules every human-readable render
// must honour: the output is never the indented-JSON fallback (the first
// non-empty line is not "{", and no JSON envelope key leaks into stdout),
// and it ends with the mandatory attribution line (ARRANQUE section 2.1).
func assertTextRender(t *testing.T, out string) {
	t.Helper()
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || strings.HasPrefix(trimmed, "{") {
		t.Fatalf("the text output must not be the JSON fallback: %.120s", out)
	}
	for _, key := range []string{`"schema_version"`, `"attribution"`, `"meta"`} {
		if strings.Contains(out, key) {
			t.Errorf("the text output must not be the JSON fallback, found %s: %.200s", key, out)
		}
	}
	if !strings.HasSuffix(out, attributionClosingLine) {
		t.Errorf("the text output must end with the attribution line, got %q", out)
	}
}

// TestIncidentsListTextRendersReadableRows pins the human-readable default
// of `trafico incidents` (FORMAT-02): a heading with the count and one
// aligned row per incident with road, PK, type, severity and description,
// never the indented-JSON fallback.
func TestIncidentsListTextRendersReadableRows(t *testing.T) {
	setCLIEnv(t)
	srv := newIncidentsServer(t, incidentsBody(t))
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "incidents", "--limit", "5")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "Incidencias (5):") {
		t.Errorf("the heading must announce the limited count, got %.200s", out)
	}
	// The snapshot publishes incidents on A-372 and CT-34 (the roads of the
	// route fixtures); a human needs at least one of them as a row.
	if !strings.Contains(out, "A-372") && !strings.Contains(out, "CT-34") {
		t.Errorf("the text output must carry a road row, got %.200s", out)
	}
}

// TestIncidentsShowTextRendersLabelledFields pins the human-readable default
// of `trafico incidents show <id>` (FORMAT-02): labelled Spanish fields,
// never the indented-JSON fallback.
func TestIncidentsShowTextRendersLabelledFields(t *testing.T) {
	setCLIEnv(t)
	srv := newIncidentsServer(t, incidentsBody(t))
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "incidents", "show", "dgt:18811074")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	for _, want := range []string{
		"Incidencia dgt:18811074",
		"Tipo:",
		"Severidad:",
		"Estado:",
		"Carretera:",
		"Inicio:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the text output misses %q; output:\n%s", want, out)
		}
	}
}

// TestSignsTextRendersReadableRows pins the human-readable default of
// `trafico signs` (FORMAT-02): a heading with the count and one row per
// panel, never the indented-JSON fallback.
func TestSignsTextRendersReadableRows(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newSignsCLIServer(t)
	writeSignsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "signs", "--limit", "5")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "Paneles de mensaje variable (5):") {
		t.Errorf("the heading must announce the limited count, got %.200s", out)
	}
}

// TestRadarsTextRendersReadableRows pins the human-readable default of
// `trafico radars` (FORMAT-02): a heading with the count and one row per
// radar, never the indented-JSON fallback.
func TestRadarsTextRendersReadableRows(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newRadarsCLIServer(t)
	writeRadarsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "radars")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "Radares fijos (12):") {
		t.Errorf("the heading must announce the 12 radars, got %.200s", out)
	}
}

// TestSectionsTextRendersReadableRows pins the human-readable default of
// `trafico sections --kind invive` (FORMAT-02): a heading with the count and
// one row per section, never the indented-JSON fallback.
func TestSectionsTextRendersReadableRows(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newSectionsCLIServer(t)
	writeSectionsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "sections", "--kind", "invive", "--limit", "5")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	// The INVIVE fixture publishes three sections; the heading names the
	// dataset and announces that count.
	if !strings.Contains(out, "Tramos invive (3):") {
		t.Errorf("the heading must name the dataset and the count, got %.200s", out)
	}
}

// TestRouteCheckTextRendersSegmentBlocks pins the human-readable default of
// `trafico route check` (FORMAT-02): one block per segment in declaration
// order, the summary at the end, never the indented-JSON fallback.
func TestRouteCheckTextRendersSegmentBlocks(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newCLIServer(t)
	writeRouteConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "route", "check", "trabajo")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	for _, want := range []string{"Ruta trabajo", "A-372", "CT-34", "Resumen:"} {
		if !strings.Contains(out, want) {
			t.Errorf("the text output misses %q; output:\n%s", want, out)
		}
	}
	// Declaration order: the A-372 block must come before the CT-34 block.
	if strings.Index(out, "A-372") > strings.Index(out, "CT-34") {
		t.Errorf("the segment blocks must follow declaration order; output:\n%s", out)
	}
}

// TestRouteListTextRendersReadableRows pins the human-readable default of
// `trafico route list` (FORMAT-02): a heading with the count and one row per
// saved route, never the indented-JSON fallback.
func TestRouteListTextRendersReadableRows(t *testing.T) {
	setCLIEnv(t)
	writeRouteConfig(t, "http://localhost:1")

	code, stdout, stderr := runCLIDefaultFormat(t, "route", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "Rutas guardadas (1):") {
		t.Errorf("the heading must announce the route count, got %.200s", out)
	}
	if !strings.Contains(out, "trabajo") || !strings.Contains(out, "A-372") {
		t.Errorf("the text output must carry the trabajo route row, got %.200s", out)
	}
}

// TestRouteShowTextRendersLabelledFields pins the human-readable default of
// `trafico route show <name>` (FORMAT-02): labelled Spanish fields, never
// the indented-JSON fallback.
func TestRouteShowTextRendersLabelledFields(t *testing.T) {
	setCLIEnv(t)
	writeRouteConfig(t, "http://localhost:1")

	code, stdout, stderr := runCLIDefaultFormat(t, "route", "show", "trabajo")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	for _, want := range []string{"Ruta trabajo", "Dirección:", "Tramos:", "A-372"} {
		if !strings.Contains(out, want) {
			t.Errorf("the text output misses %q; output:\n%s", want, out)
		}
	}
}

// TestFeedsListTextRendersReadableRows pins the human-readable default of
// `trafico feeds list` (FORMAT-02): a heading with the count and one aligned
// status row per feed, never the indented-JSON fallback.
func TestFeedsListTextRendersReadableRows(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newCLIServer(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "feeds", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	// The compiled defaults add feeds to the two of the fixture config: the
	// heading announces whatever is configured, and both fixture feeds must
	// have a row.
	if !strings.Contains(out, "Feeds (") {
		t.Errorf("the heading must announce the feed count, got %.200s", out)
	}
	if !strings.Contains(out, "dgt-situations") || !strings.Contains(out, "dgt-radars") {
		t.Errorf("the text output must carry one row per feed, got %.200s", out)
	}
	if !strings.Contains(out, srv.URL) {
		t.Errorf("feeds list is the static view: it must show the feed URL, got %.200s", out)
	}
}

// TestFeedsStatusTextRendersReadableRows pins the human-readable default of
// `trafico feeds status` (FORMAT-02): one aligned status row per feed, with
// the broken feed marked as an error, never the indented-JSON fallback.
func TestFeedsStatusTextRendersReadableRows(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newCLIServer(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "feeds", "status")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "dgt-situations") {
		t.Errorf("the text output must carry a row per feed, got %.200s", out)
	}
	// The /broken feed of the fixture answers 500: its row must say error.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "dgt-radars") {
			if !strings.Contains(line, "error") {
				t.Errorf("the broken feed row must say error, got %q", line)
			}
			return
		}
	}
	t.Errorf("the text output must carry a row for dgt-radars, got %.200s", out)
}

// TestDoctorTextRendersDiagnostics pins the human-readable default of
// `trafico doctor` (FORMAT-02): the config, feed and cache diagnostics and
// the not-an-official-source warning, never the indented-JSON fallback.
func TestDoctorTextRendersDiagnostics(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newCLIServer(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLIDefaultFormat(t, "doctor")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	for _, want := range []string{"Configuración:", "Feeds:", "Caché:", "no es una fuente oficial", "Estado:"} {
		if !strings.Contains(out, want) {
			t.Errorf("the text output misses %q; output:\n%s", want, out)
		}
	}
}

// TestWatchListTextRendersReadableRows pins the human-readable default of
// `trafico watch list` (FORMAT-02): a heading with the count and one row per
// saved watch, never the indented-JSON fallback.
func TestWatchListTextRendersReadableRows(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)

	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo",
		"--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "Vigilancias (1):") {
		t.Errorf("the heading must announce the watch count, got %.200s", out)
	}
	if !strings.Contains(out, "ruta") || !strings.Contains(out, "trabajo") {
		t.Errorf("the text output must carry the saved watch row, got %.200s", out)
	}
}

// TestWatchShowTextRendersLabelledFields pins the human-readable default of
// `trafico watch show <name>` (FORMAT-02): labelled Spanish fields, never
// the indented-JSON fallback.
func TestWatchShowTextRendersLabelledFields(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)

	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo",
		"--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}

	code, stdout, stderr := runCLIDefaultFormat(t, "watch", "show", "ruta")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	for _, want := range []string{"Vigilancia ruta", "Objetivo:", "Ventanas:", "Intervalo:"} {
		if !strings.Contains(out, want) {
			t.Errorf("the text output misses %q; output:\n%s", want, out)
		}
	}
}

// TestTextRenderTruncatesAndCounts pins the truncation rule of FORMAT-02:
// long free text is cut at a word boundary with an ellipsis, and the render
// says how many texts were truncated.
func TestTextRenderTruncatesAndCounts(t *testing.T) {
	setCLIEnv(t)
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	longURL := "https://datos.prueba.ejemplo.example.invalid/feeds/dgt/situaciones/publicacion/resultado?token=abcdefghijklmnopqrstuvwxyz0123456789"
	content := "[feeds.dgt-situations]\nurl = \"" + longURL + "\"\nenabled = true\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}

	code, stdout, stderr := runCLIDefaultFormat(t, "feeds", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	assertTextRender(t, out)
	if !strings.Contains(out, "…") {
		t.Errorf("a long URL must be truncated with an ellipsis, got %.200s", out)
	}
	if !strings.Contains(out, "descripciones truncadas") {
		t.Errorf("the render must say how many texts were truncated, got %.400s", out)
	}
	if strings.Contains(out, longURL) {
		t.Errorf("the long URL must be truncated, not shown in full, got %.200s", out)
	}
}

// TestFormatPKIntervalTakesValuesNotPointers pins the shape of the bounds:
// routeCheckPK and the watch road target both store float64, so this call
// site can never be handed a nil. If either type becomes a pointer this test
// stops compiling, which is the point -- a native review once reported the
// route check call site as a nil dereference and it was not.
func TestFormatPKIntervalTakesValuesNotPointers(t *testing.T) {
	if got, want := formatPKInterval(40, 55), "40-55"; got != want {
		t.Errorf("formatPKInterval(40, 55) = %q, want %q", got, want)
	}
	if got, want := formatPKInterval(0, 2.16), "0-2.16"; got != want {
		t.Errorf("formatPKInterval(0, 2.16) = %q, want %q", got, want)
	}
}
