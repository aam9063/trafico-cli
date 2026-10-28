package app

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/geo"
)

// Place is one saved location of the JSON contract of the `trafico place`
// commands (ROUTE-01). Coordinates are the parsed numbers; the stored
// "lat,lon" text of config.Places is a persistence detail and never travels
// anywhere (P-07: saved locations live only in the local 0600 config).
type Place struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// Places lists every saved place, in name order, with its coordinates
// parsed through the same resolver the --near filter uses (geo.ResolvePlace),
// so the list can never drift from what the filter accepts.
func Places(cfg *config.Config) ([]Place, error) {
	if cfg == nil {
		return nil, errs.NewInternal("no hay configuración para listar los lugares")
	}
	names := make([]string, 0, len(cfg.Places))
	for name := range cfg.Places {
		names = append(names, name)
	}
	sort.Strings(names)

	places := make([]Place, 0, len(names))
	for _, name := range names {
		coords, err := geo.ResolvePlace(name, cfg.Places)
		if err != nil {
			// The config was validated on load, so a malformed value here
			// is unreachable through the CLI; surface it as the usage error
			// geo.ResolvePlace produces rather than hiding it.
			return nil, err
		}
		places = append(places, Place{Name: name, Lat: coords.Latitude, Lon: coords.Longitude})
	}
	return places, nil
}

// AddPlace saves a place and persists the config. The write goes through
// config.Save (atomic, 0600) — no other code writes the config file — and
// an invalid name or coordinate is a usage error (exit 2) before anything
// is written, so a rejected add leaves the file byte-identical.
func AddPlace(cfg *config.Config, configPath, name string, lat, lon float64) error {
	if cfg == nil {
		return errs.NewInternal("no hay configuración para guardar el lugar")
	}
	if err := config.ValidatePlaceName(name); err != nil {
		return errs.NewUsage(err.Error())
	}
	if err := validateCoordinates(lat, lon); err != nil {
		return err
	}
	cfg.Places[name] = formatCoordinates(lat, lon)
	if err := config.Save(configPath, cfg); err != nil {
		// Keep the in-memory config consistent with the disk after a failed
		// atomic write.
		delete(cfg.Places, name)
		return err
	}
	return nil
}

// RemovePlace deletes a saved place and persists the config through
// config.Save. An unknown place is a not_found error (exit 4).
func RemovePlace(cfg *config.Config, configPath, name string) error {
	if cfg == nil {
		return errs.NewInternal("no hay configuración para eliminar el lugar")
	}
	if _, ok := cfg.Places[name]; !ok {
		return errs.NewNotFound(fmt.Sprintf("no existe el lugar %q", name))
	}
	value := cfg.Places[name]
	delete(cfg.Places, name)
	if err := config.Save(configPath, cfg); err != nil {
		cfg.Places[name] = value
		return err
	}
	return nil
}

// validateCoordinates enforces the same ranges as geo.ResolvePlace on read:
// latitude in [-90, 90], longitude in [-180, 180], both finite. A non-finite
// value would slip past every range comparison, so it is rejected explicitly.
func validateCoordinates(lat, lon float64) error {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) ||
		lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return errs.NewUsage("las coordenadas están fuera de rango: latitud -90..90, longitud -180..180")
	}
	return nil
}

// formatCoordinates renders coordinates the way geo.ResolvePlace reads them
// back: "lat,lon" with the shortest decimal form that round-trips.
func formatCoordinates(lat, lon float64) string {
	return strconv.FormatFloat(lat, 'f', -1, 64) + "," + strconv.FormatFloat(lon, 'f', -1, 64)
}
