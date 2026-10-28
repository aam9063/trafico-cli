package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"
)

// notOfficialWarning is the "no es una fuente oficial" warning that
// docs/ARRANQUE.md §13 requires doctor to carry. It travels as a field of
// the report, not as prose on stderr, so agents can relay it.
const notOfficialWarning = "Aviso: trafico-cli no es una fuente oficial. Los datos proceden de la DGT (Punto de Acceso Nacional, licencia CC BY) y no sustituyen a los canales de información oficial de tráfico."

// DoctorConfig is the config section of the doctor report.
type DoctorConfig struct {
	// Path is the location of the TOML configuration file.
	Path string `json:"path"`
	// Exists reports whether the file is present. A missing file is not an
	// error: the compiled defaults apply (P-03).
	Exists bool `json:"exists"`
	// Loaded reports whether the file parses and validates (also true when
	// the defaults are used because the file is missing).
	Loaded bool `json:"loaded"`
	// PermissionsOK reports that the file is not group or world readable
	// (P-07). Always true on Windows, where access is governed by ACLs
	// instead of POSIX bits.
	PermissionsOK bool `json:"permissions_ok"`
	// Error carries the load error, if any; null when the config loaded.
	Error *string `json:"error"`
}

// DoctorCache is the cache section of the doctor report.
type DoctorCache struct {
	// Dir is the cache directory under $XDG_CACHE_HOME/trafico-cli/.
	Dir string `json:"dir"`
	// Entries is the number of cached feed snapshots.
	Entries int `json:"entries"`
	// TotalBytes is the total size of the cache entries in bytes.
	TotalBytes int64 `json:"total_bytes"`
}

// DoctorReport is the output of `trafico doctor` (docs/ARRANQUE.md §5.1,
// §13): config, feeds and cache health, plus the not-an-official-source
// warning. It embeds the shared envelope so schema_version, meta and
// attribution travel with it (P-05, P-09).
type DoctorReport struct {
	output.Envelope
	Config      DoctorConfig `json:"config"`
	Feeds       []FeedStatus `json:"feeds"`
	Cache       DoctorCache  `json:"cache"`
	NotOfficial string       `json:"not_official"`
	// OK summarises whether everything critical is healthy: a usable
	// config and no enabled feed in error. A missing or disabled feed, or
	// an empty cache (first run), is not unhealthy.
	OK bool `json:"ok"`
}

// Doctor checks configuration, feeds and cache health (CMD-02). It is a
// diagnostic: it never fails because one feed is down. cfg may be nil when
// the caller could not load the config (then Doctor re-checks the file
// itself and skips the feeds section); fetcher may be nil in that case too.
func Doctor(ctx context.Context, cfg *config.Config, paths config.Paths, fetcher *feeds.Fetcher) (DoctorReport, error) {
	report := DoctorReport{Feeds: []FeedStatus{}}

	// Config: exists, loads, private enough.
	report.Config.Path = paths.ConfigPath
	info, err := os.Stat(paths.ConfigPath)
	switch {
	case err == nil && info.IsDir():
		return report, errs.NewInternal(fmt.Sprintf("la configuración %s es un directorio, no un fichero TOML", paths.ConfigPath))
	case err == nil:
		report.Config.Exists = true
	case errors.Is(err, fs.ErrNotExist):
		// Missing file: the defaults apply, nothing to report.
	default:
		return report, errs.NewInternal(fmt.Sprintf("no se pudo examinar la configuración %s: %v", paths.ConfigPath, err))
	}
	loadedCfg, loadErr := config.Load(paths.ConfigPath)
	report.Config.Loaded = loadErr == nil
	if loadErr != nil {
		message := loadErr.Error()
		report.Config.Error = &message
	}
	report.Config.PermissionsOK = configPermissionsOK(paths.ConfigPath, report.Config.Exists)
	if cfg == nil {
		// The caller could not load it; fall back to what Doctor read.
		cfg = loadedCfg
	}

	// Feeds: reachable + freshness for every enabled feed, reusing
	// FeedStatus. Skipped when there is no usable config or fetcher.
	if cfg != nil && fetcher != nil {
		list, err := FeedsStatus(ctx, cfg, fetcher)
		if err != nil {
			return report, err
		}
		report.Feeds = list
	}

	// Cache: directory, entries, total size.
	report.Cache.Dir = paths.CacheDir
	entries, totalBytes, err := cacheSummary(paths.CacheDir)
	if err != nil {
		return report, err
	}
	report.Cache.Entries = entries
	report.Cache.TotalBytes = totalBytes

	report.Envelope = output.NewEnvelope(
		DeriveMeta(report.Feeds, time.Now().UTC(), cfg != nil && fetcher != nil),
		output.NewAttribution(time.Now().UTC()),
	)
	report.NotOfficial = notOfficialWarning
	report.OK = report.Config.Loaded && report.Config.PermissionsOK && feedsHealthy(report.Feeds)
	return report, nil
}

// configPermissionsOK reports whether the config file keeps its contents
// private (P-07): on POSIX, no group or world permission bits; on Windows,
// always true because access is governed by ACLs. A missing file is not a
// permissions problem: the defaults apply.
func configPermissionsOK(path string, exists bool) bool {
	if !exists {
		return true
	}
	if runtime.GOOS == "windows" {
		return true
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Perm()&0o077 == 0
}

// cacheSummary counts the cache entries and their total size. A missing
// cache directory is a first run, not an error.
func cacheSummary(dir string) (int, int64, error) {
	dirEntries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, errs.NewInternal(fmt.Sprintf("no se pudo leer el directorio de caché %s: %v", dir, err))
	}
	entries, total := 0, int64(0)
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !strings.HasSuffix(dirEntry.Name(), ".cache") {
			continue
		}
		info, err := dirEntry.Info()
		if err != nil {
			continue
		}
		entries++
		total += info.Size()
	}
	return entries, total, nil
}

// feedsHealthy reports whether every reported feed is ok or disabled; only
// an enabled feed in error makes doctor say ok = false.
func feedsHealthy(list []FeedStatus) bool {
	for _, status := range list {
		if status.Status != FeedStatusOK && status.Status != FeedStatusDisabled {
			return false
		}
	}
	return true
}
