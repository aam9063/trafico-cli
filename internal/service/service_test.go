package service

import (
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner records every scheduler command and can be told to fail, so the
// tests never invoke a real systemctl or launchctl.
type fakeRunner struct {
	calls  []string
	fail   error
	failOn string // when set, only a call starting with this name fails
}

func (r *fakeRunner) Run(name string, args ...string) error {
	if r.failOn != "" {
		r.calls = append(r.calls, name)
		if len(args) > 0 && args[0] == r.failOn {
			return errors.New("fake runner: fallo simulado en " + name)
		}
		return nil
	}
	call := name
	for _, arg := range args {
		call += " " + arg
	}
	r.calls = append(r.calls, call)
	return r.fail
}

func (r *fakeRunner) has(call string) bool {
	for _, c := range r.calls {
		if c == call {
			return true
		}
	}
	return false
}

// testEnv returns an environment rooted at a temp home with a fixed binary
// path and the given runner, so every generated path is deterministic.
func testEnv(t *testing.T, runner *fakeRunner) Env {
	t.Helper()
	return Env{
		HomeDir: t.TempDir(),
		BinPath: "/usr/local/bin/trafico",
		Runner:  runner,
	}
}

// fileSet walks root and returns every file and directory path relative to
// it, so a test can compare the whole tree before and after an operation.
func fileSet(t *testing.T, root string) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		set[rel] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return set
}

func assertSameFileSet(t *testing.T, want, got map[string]bool) {
	t.Helper()
	for path := range want {
		if !got[path] {
			t.Errorf("path disappeared: %s", path)
		}
	}
	for path := range got {
		if !want[path] {
			t.Errorf("leftover path: %s", path)
		}
	}
}

// TestUnitLinuxExactContents pins the exact bytes of the systemd user units
// for a known home, binary and interval: the content carries no dates, no
// hostnames and nothing derived from the running machine beyond the injected
// environment (WATCH-02).
func TestUnitLinuxExactContents(t *testing.T) {
	env := Env{HomeDir: "/home/usuario", BinPath: "/usr/local/bin/trafico"}
	units, err := Unit(env, PlatformLinux, Options{Interval: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Unit(linux) failed: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("Unit(linux) returned %d units, want 2 (service and timer)", len(units))
	}
	wantService := "[Unit]\n" +
		"Description=trafico: comprobación de las vigilancias guardadas\n" +
		"\n" +
		"[Service]\n" +
		"Type=oneshot\n" +
		"ExecStart=/usr/local/bin/trafico watch run\n"
	wantTimer := "[Unit]\n" +
		"Description=trafico: temporizador de la comprobación de las vigilancias\n" +
		"\n" +
		"[Timer]\n" +
		"OnBootSec=5m\n" +
		"OnUnitActiveSec=5m\n" +
		"Unit=trafico-watch.service\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=timers.target\n"
	if units[0].Path != filepath.Join("/home/usuario", ".config", "systemd", "user", "trafico-watch.service") {
		t.Errorf("service path = %q", units[0].Path)
	}
	if units[0].Content != wantService {
		t.Errorf("service content =\n%q\nwant\n%q", units[0].Content, wantService)
	}
	if units[1].Path != filepath.Join("/home/usuario", ".config", "systemd", "user", "trafico-watch.timer") {
		t.Errorf("timer path = %q", units[1].Path)
	}
	if units[1].Content != wantTimer {
		t.Errorf("timer content =\n%q\nwant\n%q", units[1].Content, wantTimer)
	}
}

// TestUnitLinuxCarriesCustomInterval pins the interval rendering of the
// timer: a whole number of minutes uses the m unit, anything else seconds.
func TestUnitLinuxCarriesCustomInterval(t *testing.T) {
	env := Env{HomeDir: "/home/usuario", BinPath: "/usr/local/bin/trafico"}
	units, err := Unit(env, PlatformLinux, Options{Interval: 90 * time.Second})
	if err != nil {
		t.Fatalf("Unit(linux, 90s) failed: %v", err)
	}
	timer := units[1].Content
	if !strings.Contains(timer, "OnBootSec=90s\n") || !strings.Contains(timer, "OnUnitActiveSec=90s\n") {
		t.Errorf("timer does not carry the 90s interval:\n%s", timer)
	}
	units, err = Unit(env, PlatformLinux, Options{Interval: 10 * time.Minute})
	if err != nil {
		t.Fatalf("Unit(linux, 10m) failed: %v", err)
	}
	timer = units[1].Content
	if !strings.Contains(timer, "OnBootSec=10m\n") || !strings.Contains(timer, "OnUnitActiveSec=10m\n") {
		t.Errorf("timer does not carry the 10m interval:\n%s", timer)
	}
}

// TestUnitDarwinPlistIsXMLWithIntervalAndBinary generates the LaunchAgent,
// parses it as XML and checks the label, the interval, RunAtLoad and the
// program arguments.
func TestUnitDarwinPlistIsXMLWithIntervalAndBinary(t *testing.T) {
	env := Env{HomeDir: "/Users/usuario", BinPath: "/usr/local/bin/trafico"}
	units, err := Unit(env, PlatformDarwin, Options{Interval: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Unit(darwin) failed: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("Unit(darwin) returned %d units, want 1 (the plist)", len(units))
	}
	want := filepath.Join("/Users/usuario", "Library", "LaunchAgents", "com.aam9063.trafico.watch.plist")
	if units[0].Path != want {
		t.Errorf("plist path = %q, want %q", units[0].Path, want)
	}
	content := units[0].Content
	if !strings.HasPrefix(content, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>") {
		t.Errorf("plist does not start with the XML declaration:\n%s", content)
	}
	if !strings.Contains(content, "<!DOCTYPE plist PUBLIC") {
		t.Errorf("plist carries no plist DOCTYPE:\n%s", content)
	}
	values, arrays := parsePlistDict(t, content)
	if values["Label"] != "com.aam9063.trafico.watch" {
		t.Errorf("plist Label = %q", values["Label"])
	}
	if values["StartInterval"] != "300" {
		t.Errorf("plist StartInterval = %q, want 300", values["StartInterval"])
	}
	if values["RunAtLoad"] != "false" {
		t.Errorf("plist RunAtLoad = %q, want false", values["RunAtLoad"])
	}
	args, ok := arrays["ProgramArguments"]
	if !ok || len(args) != 3 || args[0] != "/usr/local/bin/trafico" || args[1] != "watch" || args[2] != "run" {
		t.Errorf("plist ProgramArguments = %v, want [/usr/local/bin/trafico watch run]", args)
	}
}

// TestUnitDarwinPlistCarriesCustomInterval pins the StartInterval rendering
// of a non-default interval.
func TestUnitDarwinPlistCarriesCustomInterval(t *testing.T) {
	env := Env{HomeDir: "/Users/usuario", BinPath: "/usr/local/bin/trafico"}
	units, err := Unit(env, PlatformDarwin, Options{Interval: 90 * time.Second})
	if err != nil {
		t.Fatalf("Unit(darwin, 90s) failed: %v", err)
	}
	values, _ := parsePlistDict(t, units[0].Content)
	if values["StartInterval"] != "90" {
		t.Errorf("plist StartInterval = %q, want 90", values["StartInterval"])
	}
}

// TestUnitWindowsReturnsUnsupportedError pins the typed error for the
// platform without a scheduler integration.
func TestUnitWindowsReturnsUnsupportedError(t *testing.T) {
	env := Env{HomeDir: "/home/usuario", BinPath: "/usr/local/bin/trafico"}
	units, err := Unit(env, PlatformWindows, Options{Interval: 5 * time.Minute})
	if units != nil {
		t.Errorf("Unit(windows) returned units %v, want nil", units)
	}
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Unit(windows) error = %v, want *UnsupportedError", err)
	}
	if unsupported.Platform != PlatformWindows {
		t.Errorf("unsupported platform = %q, want windows", unsupported.Platform)
	}
	if !strings.Contains(err.Error(), "windows") {
		t.Errorf("error message does not name the platform: %q", err.Error())
	}
}

// TestInstallLinuxWritesUnitsAndEnablesTimer installs on Linux and checks
// the files on disk plus the exact scheduler commands: daemon-reload and
// enable --now of the timer.
func TestInstallLinuxWritesUnitsAndEnablesTimer(t *testing.T) {
	runner := &fakeRunner{}
	env := testEnv(t, runner)
	written, err := Install(env, PlatformLinux, Options{Interval: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Install(linux) failed: %v", err)
	}
	servicePath := filepath.Join(env.HomeDir, ".config", "systemd", "user", "trafico-watch.service")
	timerPath := filepath.Join(env.HomeDir, ".config", "systemd", "user", "trafico-watch.timer")
	wantWritten := []string{servicePath, timerPath}
	if len(written) != 2 || written[0] != wantWritten[0] || written[1] != wantWritten[1] {
		t.Errorf("Install wrote %v, want %v", written, wantWritten)
	}
	for _, path := range wantWritten {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("unit file %s was not written: %v", path, err)
		}
	}
	if !runner.has("systemctl --user daemon-reload") {
		t.Errorf("install did not run daemon-reload; commands: %v", runner.calls)
	}
	if !runner.has("systemctl --user enable --now trafico-watch.timer") {
		t.Errorf("install did not enable the timer; commands: %v", runner.calls)
	}
}

// TestInstallDarwinWritesPlistAndLoadsIt installs on macOS and checks the
// plist on disk plus the exact load command with the injected path.
func TestInstallDarwinWritesPlistAndLoadsIt(t *testing.T) {
	runner := &fakeRunner{}
	env := testEnv(t, runner)
	written, err := Install(env, PlatformDarwin, Options{Interval: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Install(darwin) failed: %v", err)
	}
	plistPath := filepath.Join(env.HomeDir, "Library", "LaunchAgents", "com.aam9063.trafico.watch.plist")
	if len(written) != 1 || written[0] != plistPath {
		t.Errorf("Install wrote %v, want [%s]", written, plistPath)
	}
	if _, err := os.Stat(plistPath); err != nil {
		t.Errorf("plist %s was not written: %v", plistPath, err)
	}
	if !runner.has("launchctl load " + plistPath) {
		t.Errorf("install did not load the agent; commands: %v", runner.calls)
	}
}

// TestInstallThenStatusReportsInstalledAndActive runs install and reads the
// state back through Status: installed, and active because the fake
// scheduler reports success.
func TestInstallThenStatusReportsInstalledAndActive(t *testing.T) {
	for _, tt := range []struct {
		name     string
		platform Platform
	}{
		{"linux", PlatformLinux},
		{"darwin", PlatformDarwin},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := testEnv(t, runner)
			if _, err := Install(env, tt.platform, Options{Interval: 5 * time.Minute}); err != nil {
				t.Fatalf("Install(%s) failed: %v", tt.platform, err)
			}
			st, err := Status(env, tt.platform)
			if err != nil {
				t.Fatalf("Status(%s) failed: %v", tt.platform, err)
			}
			if !st.Installed {
				t.Errorf("Status after install reports Installed = false, want true")
			}
			if !st.Active {
				t.Errorf("Status after install reports Active = false, want true")
			}
			if len(st.Files) == 0 {
				t.Errorf("Status after install reports no files")
			}
		})
	}
}

// TestStatusNotInstalledIsActiveFalse reads the status of a home with no
// units: not installed, not active, no runner call.
func TestStatusNotInstalledIsActiveFalse(t *testing.T) {
	for _, tt := range []struct {
		name     string
		platform Platform
	}{
		{"linux", PlatformLinux},
		{"darwin", PlatformDarwin},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := testEnv(t, runner)
			st, err := Status(env, tt.platform)
			if err != nil {
				t.Fatalf("Status(%s) on an empty home failed: %v", tt.platform, err)
			}
			if st.Installed {
				t.Errorf("Status without install reports Installed = true, want false")
			}
			if st.Active {
				t.Errorf("Status without install reports Active = true, want false")
			}
			if len(st.Files) != 0 {
				t.Errorf("Status without install reports files %v, want none", st.Files)
			}
			if len(runner.calls) != 0 {
				t.Errorf("Status without install ran scheduler commands %v, want none", runner.calls)
			}
		})
	}
}

// TestStatusWhenSchedulerReportsInactive checks that an installed unit whose
// scheduler answers with a failure (systemctl is-active exits non-zero when
// inactive) is reported installed but not active.
func TestStatusWhenSchedulerReportsInactive(t *testing.T) {
	runner := &fakeRunner{}
	env := testEnv(t, runner)
	if _, err := Install(env, PlatformLinux, Options{Interval: 5 * time.Minute}); err != nil {
		t.Fatalf("Install(linux) failed: %v", err)
	}
	runner.fail = errors.New("inactive")
	st, err := Status(env, PlatformLinux)
	if err != nil {
		t.Fatalf("Status(linux) failed: %v", err)
	}
	if !st.Installed {
		t.Errorf("Installed = false, want true")
	}
	if st.Active {
		t.Errorf("Active = true, want false when the scheduler reports inactive")
	}
}

// TestUninstallThenStatusReportsNotInstalled installs, uninstalls and reads
// the state back: not installed and not active.
func TestUninstallThenStatusReportsNotInstalled(t *testing.T) {
	for _, tt := range []struct {
		name     string
		platform Platform
	}{
		{"linux", PlatformLinux},
		{"darwin", PlatformDarwin},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := testEnv(t, runner)
			if _, err := Install(env, tt.platform, Options{Interval: 5 * time.Minute}); err != nil {
				t.Fatalf("Install(%s) failed: %v", tt.platform, err)
			}
			removed, err := Uninstall(env, tt.platform)
			if err != nil {
				t.Fatalf("Uninstall(%s) failed: %v", tt.platform, err)
			}
			if len(removed) == 0 {
				t.Errorf("Uninstall removed nothing, want the installed files")
			}
			st, err := Status(env, tt.platform)
			if err != nil {
				t.Fatalf("Status(%s) after uninstall failed: %v", tt.platform, err)
			}
			if st.Installed {
				t.Errorf("Status after uninstall reports Installed = true, want false")
			}
			if st.Active {
				t.Errorf("Status after uninstall reports Active = true, want false")
			}
		})
	}
}

// TestUninstallLeavesNoLeftovers is the acceptance criterion of spec S9:
// after install + uninstall the whole directory tree of the injected home is
// byte-for-byte the same file set as before the install, on both platforms.
func TestUninstallLeavesNoLeftovers(t *testing.T) {
	for _, tt := range []struct {
		name     string
		platform Platform
	}{
		{"linux", PlatformLinux},
		{"darwin", PlatformDarwin},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := testEnv(t, runner)
			before := fileSet(t, env.HomeDir)
			if _, err := Install(env, tt.platform, Options{Interval: 5 * time.Minute}); err != nil {
				t.Fatalf("Install(%s) failed: %v", tt.platform, err)
			}
			during := fileSet(t, env.HomeDir)
			if len(during) <= len(before) {
				t.Fatalf("Install(%s) wrote nothing under the home", tt.platform)
			}
			if _, err := Uninstall(env, tt.platform); err != nil {
				t.Fatalf("Uninstall(%s) failed: %v", tt.platform, err)
			}
			after := fileSet(t, env.HomeDir)
			assertSameFileSet(t, before, after)
		})
	}
}

// TestUninstallTwiceIsNotAnError pins idempotency: uninstalling what is not
// installed succeeds and runs no scheduler command.
func TestUninstallTwiceIsNotAnError(t *testing.T) {
	for _, tt := range []struct {
		name     string
		platform Platform
	}{
		{"linux", PlatformLinux},
		{"darwin", PlatformDarwin},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := testEnv(t, runner)
			if _, err := Install(env, tt.platform, Options{Interval: 5 * time.Minute}); err != nil {
				t.Fatalf("Install(%s) failed: %v", tt.platform, err)
			}
			if _, err := Uninstall(env, tt.platform); err != nil {
				t.Fatalf("first Uninstall(%s) failed: %v", tt.platform, err)
			}
			callsAfterFirst := len(runner.calls)
			removed, err := Uninstall(env, tt.platform)
			if err != nil {
				t.Fatalf("second Uninstall(%s) failed: %v", tt.platform, err)
			}
			if len(removed) != 0 {
				t.Errorf("second Uninstall removed %v, want nothing", removed)
			}
			if len(runner.calls) != callsAfterFirst {
				t.Errorf("second Uninstall ran commands %v, want none", runner.calls[callsAfterFirst:])
			}
		})
	}
}

// TestUninstallWithoutInstallRunsNoCommands pins that uninstalling on a
// machine where nothing was installed neither fails nor touches the
// scheduler.
func TestUninstallWithoutInstallRunsNoCommands(t *testing.T) {
	runner := &fakeRunner{}
	env := testEnv(t, runner)
	removed, err := Uninstall(env, PlatformLinux)
	if err != nil {
		t.Fatalf("Uninstall(linux) without install failed: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("Uninstall without install removed %v, want nothing", removed)
	}
	if len(runner.calls) != 0 {
		t.Errorf("Uninstall without install ran commands %v, want none", runner.calls)
	}
}

// TestUninstallDarwinUnloadsBeforeRemoving pins the exact unload command of
// the macOS uninstall, with the injected plist path.
func TestUninstallDarwinUnloadsBeforeRemoving(t *testing.T) {
	runner := &fakeRunner{}
	env := testEnv(t, runner)
	if _, err := Install(env, PlatformDarwin, Options{Interval: 5 * time.Minute}); err != nil {
		t.Fatalf("Install(darwin) failed: %v", err)
	}
	runner.calls = nil
	plistPath := filepath.Join(env.HomeDir, "Library", "LaunchAgents", "com.aam9063.trafico.watch.plist")
	if _, err := Uninstall(env, PlatformDarwin); err != nil {
		t.Fatalf("Uninstall(darwin) failed: %v", err)
	}
	if !runner.has("launchctl unload " + plistPath) {
		t.Errorf("uninstall did not unload the agent; commands: %v", runner.calls)
	}
	if runner.has("launchctl load " + plistPath) {
		t.Errorf("uninstall must never load, only unload; commands: %v", runner.calls)
	}
}

// TestUninstallLinuxDisablesTimer pins the exact disable command of the
// Linux uninstall.
func TestUninstallLinuxDisablesTimer(t *testing.T) {
	runner := &fakeRunner{}
	env := testEnv(t, runner)
	if _, err := Install(env, PlatformLinux, Options{Interval: 5 * time.Minute}); err != nil {
		t.Fatalf("Install(linux) failed: %v", err)
	}
	runner.calls = nil
	if _, err := Uninstall(env, PlatformLinux); err != nil {
		t.Fatalf("Uninstall(linux) failed: %v", err)
	}
	if !runner.has("systemctl --user disable --now trafico-watch.timer") {
		t.Errorf("uninstall did not disable the timer; commands: %v", runner.calls)
	}
	if runner.has("systemctl --user enable --now trafico-watch.timer") {
		t.Errorf("uninstall must never enable, only disable; commands: %v", runner.calls)
	}
}

// TestFailingRunnerSurfacesError pins that a scheduler command failure is
// reported, never swallowed, by Install, Uninstall and the active check of
// Status.
func TestFailingRunnerSurfacesError(t *testing.T) {
	boom := errors.New("boom")

	t.Run("install linux", func(t *testing.T) {
		runner := &fakeRunner{fail: boom}
		env := testEnv(t, runner)
		if _, err := Install(env, PlatformLinux, Options{Interval: 5 * time.Minute}); err == nil {
			t.Fatal("Install(linux) with a failing runner returned no error")
		}
	})
	t.Run("install darwin", func(t *testing.T) {
		runner := &fakeRunner{fail: boom}
		env := testEnv(t, runner)
		if _, err := Install(env, PlatformDarwin, Options{Interval: 5 * time.Minute}); err == nil {
			t.Fatal("Install(darwin) with a failing runner returned no error")
		}
	})
	t.Run("uninstall linux", func(t *testing.T) {
		runner := &fakeRunner{}
		env := testEnv(t, runner)
		if _, err := Install(env, PlatformLinux, Options{Interval: 5 * time.Minute}); err != nil {
			t.Fatalf("Install(linux) failed: %v", err)
		}
		runner.fail = boom
		if _, err := Uninstall(env, PlatformLinux); err == nil {
			t.Fatal("Uninstall(linux) with a failing runner returned no error")
		}
	})
	t.Run("uninstall darwin", func(t *testing.T) {
		runner := &fakeRunner{}
		env := testEnv(t, runner)
		if _, err := Install(env, PlatformDarwin, Options{Interval: 5 * time.Minute}); err != nil {
			t.Fatalf("Install(darwin) failed: %v", err)
		}
		runner.fail = boom
		if _, err := Uninstall(env, PlatformDarwin); err == nil {
			t.Fatal("Uninstall(darwin) with a failing runner returned no error")
		}
	})
}

// TestInstallWindowsReturnsUnsupportedError pins the typed error surface of
// Install, Uninstall and Status on Windows: nothing is written and nothing
// runs.
func TestInstallWindowsReturnsUnsupportedError(t *testing.T) {
	runner := &fakeRunner{}
	env := testEnv(t, runner)
	if _, err := Install(env, PlatformWindows, Options{Interval: 5 * time.Minute}); err == nil {
		t.Fatal("Install(windows) returned no error")
	}
	if _, err := Uninstall(env, PlatformWindows); err == nil {
		t.Fatal("Uninstall(windows) returned no error")
	}
	if _, err := Status(env, PlatformWindows); err == nil {
		t.Fatal("Status(windows) returned no error")
	}
	if len(runner.calls) != 0 {
		t.Errorf("windows runs commands %v, want none", runner.calls)
	}
	if before, during := fileSet(t, env.HomeDir), fileSet(t, env.HomeDir); len(before) != len(during) {
		t.Errorf("windows wrote to the home directory")
	}
}

// TestUninstallRemovesOnlyItsOwnFiles pins that Uninstall leaves foreign
// files alone: a user file living next to the units survives.
func TestUninstallRemovesOnlyItsOwnFiles(t *testing.T) {
	runner := &fakeRunner{}
	env := testEnv(t, runner)
	userDir := filepath.Join(env.HomeDir, ".config", "systemd", "user")
	otherPlist := filepath.Join(env.HomeDir, "Library", "LaunchAgents", "com.other.app.plist")
	for _, dir := range []string{userDir, filepath.Dir(otherPlist)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	foreign := []string{
		filepath.Join(userDir, "otro.service"),
		otherPlist,
	}
	for _, path := range foreign {
		if err := os.WriteFile(path, []byte("keep me\n"), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	for _, platform := range []Platform{PlatformLinux, PlatformDarwin} {
		if _, err := Install(env, platform, Options{Interval: 5 * time.Minute}); err != nil {
			t.Fatalf("Install(%s) failed: %v", platform, err)
		}
		if _, err := Uninstall(env, platform); err != nil {
			t.Fatalf("Uninstall(%s) failed: %v", platform, err)
		}
	}
	for _, path := range foreign {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("uninstall removed the foreign file %s: %v", path, err)
		}
	}
}

// parsePlistDict flattens the top-level dictionary of a plist document: the
// scalar value element following each <key> lands in values ("true"/"false"
// for booleans, the text for strings and integers) and the strings of an
// <array> land in arrays. The parse itself fails the test when the content
// is not well-formed XML.
func parsePlistDict(t *testing.T, content string) (values map[string]string, arrays map[string][]string) {
	t.Helper()
	values, arrays = map[string]string{}, map[string][]string{}
	dec := xml.NewDecoder(strings.NewReader(content))
	pendingKey := ""
	inKey := false
	inArray := ""
	inValue := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return values, arrays
		}
		if err != nil {
			t.Fatalf("parsing the generated plist as XML: %v", err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			switch tok.Name.Local {
			case "key":
				inKey = true
			case "array":
				inArray = pendingKey
			case "string", "integer":
				if inArray == "" {
					inValue = true
				}
			case "true":
				if inArray == "" && pendingKey != "" {
					values[pendingKey] = "true"
					pendingKey = ""
				}
			case "false":
				if inArray == "" && pendingKey != "" {
					values[pendingKey] = "false"
					pendingKey = ""
				}
			}
		case xml.CharData:
			text := strings.TrimSpace(string(tok))
			if text == "" {
				continue
			}
			switch {
			case inKey:
				pendingKey = text
				inKey = false
			case inArray != "":
				arrays[inArray] = append(arrays[inArray], text)
			case inValue:
				values[pendingKey] = text
				pendingKey = ""
				inValue = false
			}
		case xml.EndElement:
			switch tok.Name.Local {
			case "array":
				inArray = ""
			case "string", "integer":
				inValue = false
			}
		}
	}
}

// TestUninstallRemovesFilesWhenUnloadFails is the regression for the macOS hole
// the review found: an unload of a job launchd does not consider loaded exits
// non-zero, which is exactly what happens when the load at install time failed.
// Treating that as fatal left the plist on disk forever with no way out from
// the CLI, so the files must be removed anyway and the failure reported.
func TestUninstallRemovesFilesWhenUnloadFails(t *testing.T) {
	runner := &fakeRunner{failOn: "unload"}
	env := testEnv(t, runner)

	if _, err := Install(env, PlatformDarwin, Options{Interval: 5 * time.Minute}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	removed, err := Uninstall(env, PlatformDarwin)
	if err == nil {
		t.Fatal("Uninstall() = nil error, want the unload failure reported")
	}
	if len(removed) == 0 {
		t.Fatal("Uninstall() removed nothing: a failed unload must not block file removal")
	}
	if st, _ := Status(env, PlatformDarwin); st.Installed {
		t.Error("Status() reports installed after Uninstall removed the files")
	}
}
