package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Names of the unit files this package owns, per platform. They are exported
// because the CLI reports them and the docs of `watch service` describe them.
const (
	// LinuxServiceName is the oneshot systemd user unit that runs the cycle.
	LinuxServiceName = "trafico-watch.service"
	// LinuxTimerName is the systemd user timer that fires the service.
	LinuxTimerName = "trafico-watch.timer"
	// MacOSLabel is the launchd label of the LaunchAgent.
	MacOSLabel = "com.aam9063.trafico.watch"
	// MacOSName is the plist file name under ~/Library/LaunchAgents.
	MacOSName = MacOSLabel + ".plist"
)

// Platform is the operating system the scheduler integration targets. The
// values match runtime.GOOS so the CLI can pass runtime.GOOS through.
type Platform string

const (
	// PlatformLinux selects the systemd user units.
	PlatformLinux Platform = "linux"
	// PlatformDarwin selects the launchd LaunchAgent (macOS).
	PlatformDarwin Platform = "darwin"
	// PlatformWindows has no scheduler integration.
	PlatformWindows Platform = "windows"
)

// Options carries the configuration of the generated units.
type Options struct {
	// Interval is the time between two runs of the vigilance cycle. It
	// becomes OnBootSec/OnUnitActiveSec in the systemd timer and
	// StartInterval in the launchd plist.
	Interval time.Duration
}

// CommandRunner runs one scheduler control command (systemctl, launchctl) and
// returns its error. Tests inject a fake so nothing real is ever executed;
// the CLI wires SystemRunner.
type CommandRunner interface {
	// Run executes the named program with args and returns its error
	// (non-nil on a non-zero exit).
	Run(name string, args ...string) error
}

// Env is the environment the scheduler integration runs in. Everything the
// generated files need comes from here: no absolute path is derived from the
// running machine.
type Env struct {
	// HomeDir is the user home directory. The macOS LaunchAgent lives under
	// HomeDir/Library/LaunchAgents.
	HomeDir string
	// ConfigHome is the XDG config directory used for the systemd user
	// units. When empty it falls back to HomeDir/.config.
	ConfigHome string
	// BinPath is the absolute path of the trafico binary the units execute.
	BinPath string
	// Runner executes the scheduler control commands.
	Runner CommandRunner
}

// UnitFile is one generated unit file: its destination path and its exact
// content.
type UnitFile struct {
	Path    string
	Content string
}

// UnsupportedError reports that the platform has no scheduler integration.
// The CLI surfaces it as a usage error (exit 2) instead of pretending the
// installation happened.
type UnsupportedError struct {
	Platform Platform
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf(
		"trafico watch service no está soportado en %s: usa el programador de tareas del sistema para lanzar `trafico watch run` periódicamente",
		e.Platform)
}

// systemdUserDir returns the systemd user unit directory of the environment:
// $XDG_CONFIG_HOME/systemd/user, with the conventional ~/.config fallback.
func (env Env) systemdUserDir() string {
	configHome := env.ConfigHome
	if configHome == "" {
		configHome = filepath.Join(env.HomeDir, ".config")
	}
	return filepath.Join(configHome, "systemd", "user")
}

// launchAgentsDir returns the macOS LaunchAgents directory of the
// environment.
func (env Env) launchAgentsDir() string {
	return filepath.Join(env.HomeDir, "Library", "LaunchAgents")
}

// unitPaths returns the file paths Install creates and Uninstall removes on
// the platform, derived only from the injected environment.
func unitPaths(env Env, platform Platform) ([]string, error) {
	switch platform {
	case PlatformLinux:
		dir := env.systemdUserDir()
		return []string{
			filepath.Join(dir, LinuxServiceName),
			filepath.Join(dir, LinuxTimerName),
		}, nil
	case PlatformDarwin:
		return []string{filepath.Join(env.launchAgentsDir(), MacOSName)}, nil
	default:
		return nil, &UnsupportedError{Platform: platform}
	}
}

// Unit returns the exact file paths and contents of the units the platform
// scheduler needs. On Linux that is the oneshot service plus its timer under
// the systemd user unit directory; on macOS the LaunchAgent plist under
// ~/Library/LaunchAgents. On Windows it returns a typed UnsupportedError.
//
// The returned slice has one entry per file: systemd needs two files, which
// is why the return value is a slice instead of a single (path, content)
// pair. The content is fully determined by the injected environment and the
// options: no dates, no hostnames, nothing read from the running machine.
func Unit(env Env, platform Platform, opts Options) ([]UnitFile, error) {
	switch platform {
	case PlatformLinux:
		return linuxUnits(env, opts)
	case PlatformDarwin:
		return darwinUnits(env, opts)
	default:
		return nil, &UnsupportedError{Platform: platform}
	}
}

// linuxUnits builds the systemd user service and timer. The timer fires the
// service on boot and then once per interval.
func linuxUnits(env Env, opts Options) ([]UnitFile, error) {
	interval, err := systemdInterval(opts.Interval)
	if err != nil {
		return nil, err
	}
	paths, err := unitPaths(env, PlatformLinux)
	if err != nil {
		return nil, err
	}
	serviceContent := fmt.Sprintf(`[Unit]
Description=trafico: comprobación de las vigilancias guardadas

[Service]
Type=oneshot
ExecStart=%s watch run
`, env.BinPath)
	timerContent := fmt.Sprintf(`[Unit]
Description=trafico: temporizador de la comprobación de las vigilancias

[Timer]
OnBootSec=%s
OnUnitActiveSec=%s
Unit=%s

[Install]
WantedBy=timers.target
`, interval, interval, LinuxServiceName)
	return []UnitFile{
		{Path: paths[0], Content: serviceContent},
		{Path: paths[1], Content: timerContent},
	}, nil
}

// systemdInterval renders a duration in the unit syntax systemd accepts:
// whole minutes as <n>m, whole seconds otherwise.
func systemdInterval(d time.Duration) (string, error) {
	if d <= 0 {
		return "", fmt.Errorf("service: el intervalo del temporizador debe ser positivo, recibido %s", d)
	}
	if d%time.Second != 0 {
		return "", fmt.Errorf("service: el intervalo del temporizador debe ser un múltiplo de un segundo, recibido %s", d)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute)), nil
	}
	return fmt.Sprintf("%ds", int(d/time.Second)), nil
}

// darwinUnits builds the launchd LaunchAgent: the vigilance cycle every
// interval, nothing at load time.
func darwinUnits(env Env, opts Options) ([]UnitFile, error) {
	interval, err := launchdInterval(opts.Interval)
	if err != nil {
		return nil, err
	}
	paths, err := unitPaths(env, PlatformDarwin)
	if err != nil {
		return nil, err
	}
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
      <string>%s</string>
      <string>watch</string>
      <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <false/>
    <key>StartInterval</key>
    <integer>%d</integer>
  </dict>
</plist>
`, MacOSLabel, env.BinPath, interval)
	return []UnitFile{{Path: paths[0], Content: content}}, nil
}

// launchdInterval renders a duration as the whole seconds StartInterval
// expects.
func launchdInterval(d time.Duration) (int, error) {
	if d <= 0 {
		return 0, fmt.Errorf("service: el intervalo del LaunchAgent debe ser positivo, recibido %s", d)
	}
	if d%time.Second != 0 {
		return 0, fmt.Errorf("service: el intervalo del LaunchAgent debe ser un múltiplo de un segundo, recibido %s", d)
	}
	return int(d / time.Second), nil
}

// Install writes the unit files of the platform and asks the scheduler to
// activate them: daemon-reload plus enable --now of the timer on Linux, load
// of the plist on macOS. It returns the paths written, in the order
// unitPaths lists them. An already-installed watch is overwritten, so
// reinstalling with a new interval is the way to change one.
func Install(env Env, platform Platform, opts Options) ([]string, error) {
	units, err := Unit(env, platform, opts)
	if err != nil {
		return nil, err
	}
	written := make([]string, 0, len(units))
	for _, unit := range units {
		dir := filepath.Dir(unit.Path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return written, fmt.Errorf("service: no se pudo crear el directorio %s: %w", dir, err)
		}
		// 0600 like the rest of the trafico storage: the unit embeds the
		// binary path of this user's installation.
		if err := os.WriteFile(unit.Path, []byte(unit.Content), 0o600); err != nil {
			return written, fmt.Errorf("service: no se pudo escribir %s: %w", unit.Path, err)
		}
		written = append(written, unit.Path)
	}
	if err := activate(env, platform); err != nil {
		return written, err
	}
	return written, nil
}

// activate asks the scheduler to pick the units up and enable them. It runs
// only after the files are on disk.
func activate(env Env, platform Platform) error {
	switch platform {
	case PlatformLinux:
		if err := env.Runner.Run("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("service: systemctl --user daemon-reload falló: %w", err)
		}
		if err := env.Runner.Run("systemctl", "--user", "enable", "--now", LinuxTimerName); err != nil {
			return fmt.Errorf("service: no se pudo activar el temporizador: %w", err)
		}
	case PlatformDarwin:
		paths, err := unitPaths(env, platform)
		if err != nil {
			return err
		}
		if err := env.Runner.Run("launchctl", "load", paths[0]); err != nil {
			return fmt.Errorf("service: no se pudo cargar el LaunchAgent: %w", err)
		}
	}
	return nil
}

// Uninstall stops the scheduler entry and removes every file Install created
// and nothing else (spec S9), then cleans up the directories Install created
// when they are empty afterwards. Uninstalling what is not installed is not
// an error: it removes nothing and runs no command, which keeps a second
// uninstall a no-op.
func Uninstall(env Env, platform Platform) ([]string, error) {
	paths, err := unitPaths(env, platform)
	if err != nil {
		return nil, err
	}
	present := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			present = append(present, path)
		}
	}
	if len(present) == 0 {
		return nil, nil
	}
	// On macOS an unload of a job launchd does not consider loaded exits
	// non-zero, which is what happens whenever the load at install time failed.
	// Treating that as fatal would leave the plist on disk forever with no way
	// out from the CLI, so the files are removed anyway and the failure is
	// reported alongside them. On Linux disable is idempotent, so a failure
	// there is a real problem and still aborts.
	var deactivateErr error
	if err := deactivate(env, platform); err != nil {
		if platform != PlatformDarwin {
			return nil, err
		}
		deactivateErr = err
	}
	removed := make([]string, 0, len(present))
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return removed, fmt.Errorf("service: no se pudo eliminar %s: %w", path, err)
		}
		removed = append(removed, path)
	}
	switch platform {
	case PlatformLinux:
		if err := env.Runner.Run("systemctl", "--user", "daemon-reload"); err != nil {
			return removed, fmt.Errorf("service: systemctl --user daemon-reload falló: %w", err)
		}
	}
	removeEmptyDirChain(unitDir(platform, env), env.HomeDir)
	return removed, deactivateErr
}

// deactivate asks the scheduler to stop and forget the units. It runs only
// when at least one unit file is on disk, because disable/unload of a unit
// the scheduler does not know fails.
func deactivate(env Env, platform Platform) error {
	switch platform {
	case PlatformLinux:
		if err := env.Runner.Run("systemctl", "--user", "disable", "--now", LinuxTimerName); err != nil {
			return fmt.Errorf("service: no se pudo desactivar el temporizador: %w", err)
		}
	case PlatformDarwin:
		paths, err := unitPaths(env, platform)
		if err != nil {
			return err
		}
		if err := env.Runner.Run("launchctl", "unload", paths[0]); err != nil {
			return fmt.Errorf("service: no se pudo descargar el LaunchAgent: %w", err)
		}
	}
	return nil
}

// unitDir returns the directory of the platform's unit files, the directory
// Install creates and Uninstall cleans up when empty.
func unitDir(platform Platform, env Env) string {
	switch platform {
	case PlatformDarwin:
		return env.launchAgentsDir()
	default:
		return env.systemdUserDir()
	}
}

// removeEmptyDirChain removes dir and then its parents while each removal
// succeeds, i.e. while each directory is empty, stopping before stop. It
// guarantees Uninstall leaves no directory behind that Install created, and
// never touches directories that hold other content because os.Remove fails
// on a non-empty directory.
func removeEmptyDirChain(dir, stop string) {
	for dir != stop && dir != "." && dir != string(filepath.Separator) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// Status is the scheduler state of the watch service.
type StatusReport struct {
	// Installed is true when every unit file of the platform exists.
	Installed bool
	// Active is true when the scheduler reports the unit up. It is only
	// meaningful when Installed is true.
	Active bool
	// Files lists the unit files currently on disk.
	Files []string
}

// Status reports whether the unit files are installed and whether the
// scheduler reports the unit active. When nothing is installed the scheduler
// is never asked, so a status of a machine without the service runs no
// command.
func Status(env Env, platform Platform) (StatusReport, error) {
	paths, err := unitPaths(env, platform)
	if err != nil {
		return StatusReport{}, err
	}
	st := StatusReport{}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			st.Files = append(st.Files, path)
		}
	}
	st.Installed = len(paths) > 0 && len(st.Files) == len(paths)
	if st.Installed {
		st.Active = isActive(env, platform) == nil
	}
	return st, nil
}

// isActive asks the scheduler whether the unit is up: systemctl is-active
// exits non-zero when the timer is inactive, launchctl list when the agent
// is not loaded.
func isActive(env Env, platform Platform) error {
	switch platform {
	case PlatformLinux:
		return env.Runner.Run("systemctl", "--user", "is-active", "--quiet", LinuxTimerName)
	case PlatformDarwin:
		return env.Runner.Run("launchctl", "list", MacOSLabel)
	default:
		return nil
	}
}

// SystemRunner is the real CommandRunner backed by os/exec. Only the CLI
// wires it in; tests inject a fake so no test ever executes systemctl or
// launchctl.
type SystemRunner struct{}

// Run executes the named program and returns its error, carrying the
// combined output in the message so a scheduler failure is diagnosable.
func (SystemRunner) Run(name string, args ...string) error {
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
