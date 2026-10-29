package config

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/road"
)

// Feed names of the six DGT datasets a command can consume (docs/feeds.md §1,
// docs/ARRANQUE.md §5.1). The URLs are compiled defaults so a DGT URL change
// can be fixed in config without waiting for a release (docs/ARRANQUE.md §7).
//
// The motorcycle-risk dataset (tramosriesgomotos) is deliberately absent: it is
// a real, licensed feed, but no command reads it, and shipping a feed the CLI
// cannot use would only mislead whoever enables it.
const (
	FeedSituations      = "dgt-situations"
	FeedSigns           = "dgt-signs"
	FeedSignsMessages   = "dgt-signs-messages"
	FeedRadars          = "dgt-radars"
	FeedSectionsInvive  = "dgt-sections-invive"
	FeedSectionsAnimals = "dgt-sections-animals"
)

const (
	defaultSituationsURL = "https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml"
	defaultSignsURL      = "https://nap.dgt.es/datex2/v3/dgt/DevicePublication/vms_datex2_v37.xml"
	defaultSignsMsgURL   = "https://nap.dgt.es/datex2/v3/dgt/VmsPublication/datex2_v37.xml"
	defaultRadarsURL     = "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/radares/content.xml"
	defaultInviveURL     = "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/tramos_invive/content.xml"
	defaultAnimalsURL    = "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/tefiva/content.xml"
)

// DefaultMaxAge is the default staleness threshold (docs/ARRANQUE.md §5.5).
const DefaultMaxAge = Duration(10 * time.Minute)

// Feed is one feed entry under [feeds.<name>].
type Feed struct {
	// URL of the DATEX II XML document. Empty means "use the compiled default"
	// only in the sense of merging: an explicitly saved config carries the
	// resolved URL.
	URL string `toml:"url"`
	// Enabled controls whether the feed is downloaded.
	Enabled bool `toml:"enabled"`
}

// Sink is one notification destination under [sinks.<name>].
type Sink struct {
	// Type is one of "ntfy", "webhook" or "exec".
	Type string `toml:"type"`
	// URL is the sink endpoint (ntfy server, webhook URL or executable path).
	URL string `toml:"url"`
	// Topic is required for ntfy sinks.
	Topic string `toml:"topic"`
	// Template selects the payload shape of a webhook sink
	// ("discord", "slack" or "telegram").
	Template string `toml:"template"`
	// ChatID is the destination chat of a telegram webhook sink; required
	// for template = "telegram", ignored by every other template.
	ChatID string `toml:"chat_id"`
}

// Route is one saved route under [routes.<name>] (docs/ARRANQUE.md §5.4 and
// §7, ROUTE-01): a named list of road segments with an optional travel
// direction. Routes live in the config file, never in the SQLite store, and
// the file is 0600 because a route is personal (P-07).
type Route struct {
	// Direction is the travel direction of the route: "positive",
	// "negative" or "both". Empty means "both" (the default `route add`
	// applies).
	Direction string `toml:"direction"`
	// Segments are the road stretches of the route, in the order the user
	// declared them, each shaped "ROAD:FROM-TO" (e.g. "A-7:102-118").
	// `route check` reports them in exactly this order (§5.4).
	Segments []string `toml:"segments"`
}

// Stale groups the freshness settings under [stale].
type Stale struct {
	// MaxAge is how old a published feed may be before it is reported stale.
	MaxAge Duration `toml:"max_age"`
}

// Config is the whole trafico-cli configuration (docs/ARRANQUE.md §7).
type Config struct {
	Feeds  map[string]Feed   `toml:"feeds"`
	Places map[string]string `toml:"places"`
	Routes map[string]Route  `toml:"routes"`
	Stale  Stale             `toml:"stale"`
	Sinks  map[string]Sink   `toml:"sinks"`
}

// Duration is a time.Duration that marshals to and from TOML as a duration
// string ("10m"), the shape shown in docs/ARRANQUE.md §7, instead of a raw
// nanosecond integer.
type Duration time.Duration

// UnmarshalText parses a duration string such as "10m" or "90s".
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("config: %q is not a valid duration (e.g. \"10m\"): %w", string(text), err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalText renders the duration as a compact string such as "10m"
// (time.Duration.String would render the same value as "10m0s").
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// String renders the duration as a compact string such as "10m".
func (d Duration) String() string {
	v := time.Duration(d)
	switch {
	case v == 0:
		return "0s"
	case v%time.Minute == 0:
		return strconv.Itoa(int(v/time.Minute)) + "m"
	case v%time.Second == 0:
		return strconv.Itoa(int(v/time.Second)) + "s"
	case v%time.Millisecond == 0:
		return strconv.Itoa(int(v/time.Millisecond)) + "ms"
	default:
		return v.String()
	}
}

// Default returns the compiled configuration: every confirmed feed of
// docs/feeds.md §1 with its real URL, only dgt-situations enabled, a staleness
// threshold of 10 minutes and no places or sinks. It is the base that Load
// merges a config file over, so the tool works out of the box (P-03).
func Default() *Config {
	return &Config{
		Feeds: map[string]Feed{
			FeedSituations:      {URL: defaultSituationsURL, Enabled: true},
			FeedSigns:           {URL: defaultSignsURL, Enabled: false},
			FeedSignsMessages:   {URL: defaultSignsMsgURL, Enabled: false},
			FeedRadars:          {URL: defaultRadarsURL, Enabled: false},
			FeedSectionsInvive:  {URL: defaultInviveURL, Enabled: false},
			FeedSectionsAnimals: {URL: defaultAnimalsURL, Enabled: false},
		},
		Places: map[string]string{},
		Routes: map[string]Route{},
		Stale:  Stale{MaxAge: DefaultMaxAge},
		Sinks:  map[string]Sink{},
	}
}

// Validate reports every rule violation of the configuration at once:
// enabled feeds without a URL, malformed places, a non-positive staleness
// threshold, sinks of an unknown type and ntfy sinks without a topic.
func (c *Config) Validate() error {
	var problems []error

	for name, feed := range c.Feeds {
		if feed.Enabled && strings.TrimSpace(feed.URL) == "" {
			problems = append(problems, fmt.Errorf("feed %q is enabled but has no URL", name))
		}
	}

	for name, value := range c.Places {
		if err := validatePlace(name, value); err != nil {
			problems = append(problems, err)
		}
	}

	for name, route := range c.Routes {
		if err := ValidateRouteName(name); err != nil {
			problems = append(problems, err)
			continue
		}
		if err := validateRoute(name, route); err != nil {
			problems = append(problems, err)
		}
	}

	if c.Stale.MaxAge <= 0 {
		problems = append(problems, fmt.Errorf("stale.max_age must be positive, got %s", c.Stale.MaxAge))
	}

	for name, sink := range c.Sinks {
		switch sink.Type {
		case "ntfy":
			if strings.TrimSpace(sink.Topic) == "" {
				problems = append(problems, fmt.Errorf("sink %q: an ntfy sink requires a topic", name))
			}
		case "webhook", "exec":
			// Nothing else to enforce yet: the URL and template are the
			// user's choice.
		default:
			problems = append(problems, fmt.Errorf("sink %q: unknown type %q (want \"ntfy\", \"webhook\" or \"exec\")", name, sink.Type))
		}
	}

	switch len(problems) {
	case 0:
		return nil
	case 1:
		return problems[0]
	default:
		// Join all problems so a single pass reports every mistake.
		joined := problems[0]
		for _, p := range problems[1:] {
			joined = fmt.Errorf("%w; %v", joined, p)
		}
		return joined
	}
}

// validatePlace checks a "lat,lon" pair: two coordinates, both numeric, with
// latitude in [-90, 90] and longitude in [-180, 180].
func validatePlace(name, value string) error {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return fmt.Errorf("place %q: want \"lat,lon\", got %q", name, value)
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return fmt.Errorf("place %q: latitude %q is not a number", name, strings.TrimSpace(parts[0]))
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return fmt.Errorf("place %q: longitude %q is not a number", name, strings.TrimSpace(parts[1]))
	}
	if lat < -90 || lat > 90 {
		return fmt.Errorf("place %q: latitude %g is out of range [-90, 90]", name, lat)
	}
	if lon < -180 || lon > 180 {
		return fmt.Errorf("place %q: longitude %g is out of range [-180, 180]", name, lon)
	}
	return nil
}

// validateRoute checks one saved route: at least one segment, every segment
// parsable as "ROAD:FROM-TO" with FROM <= TO, and a direction of the
// documented set (empty counts as "both"). It runs on Load, so a malformed
// route is a loud configuration error, never a silent skip.
func validateRoute(name string, route Route) error {
	if len(route.Segments) == 0 {
		return fmt.Errorf("route %q: at least one segment is required", name)
	}
	for _, segment := range route.Segments {
		if _, err := ParseRouteSegment(segment); err != nil {
			return fmt.Errorf("route %q: %w", name, err)
		}
	}
	switch route.Direction {
	case "", "positive", "negative", "both":
		return nil
	default:
		return fmt.Errorf("route %q: unknown direction %q (want \"positive\", \"negative\" or \"both\")", name, route.Direction)
	}
}

// identifierPattern matches a safe identifier for a saved route or place
// name: a TOML bare key that is also safe to echo in messages and that can
// never read as a path or shell fragment. It must start with a letter or
// digit and continue with letters, digits, dots, hyphens or underscores, up
// to 64 characters.
var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateRouteName reports whether name is a safe identifier for a route
// key under [routes.<name>]. The route and place commands apply it to new
// entries so an unsafe name is rejected at add time (exit 2) instead of
// producing a config Load can no longer accept.
func ValidateRouteName(name string) error {
	return validateIdentifier("route", name)
}

// ValidatePlaceName applies the same safe-identifier rule to a place name.
// Existing place keys in a loaded config are never re-validated by
// Validate (the [places] section predates ROUTE-01); the rule guards new
// entries created through the place command.
func ValidatePlaceName(name string) error {
	return validateIdentifier("place", name)
}

func validateIdentifier(kind, name string) error {
	if !identifierPattern.MatchString(name) {
		return fmt.Errorf("%s name %q is not a safe identifier: use 1-64 letters, digits, dots, hyphens or underscores, starting with a letter or digit", kind, name)
	}
	return nil
}

// RouteSegment is one parsed route segment: a road with its kilometre-point
// interval, both bounds present and From <= To.
type RouteSegment struct {
	Road string
	From float64
	To   float64
}

// ParseRouteSegment parses a stored route segment "ROAD:FROM-TO" (e.g.
// "A-7:102-118"). ROAD is any road name and is normalised with
// road.Normalize, so "a7:100-140" and "A-7:100-140" are the same segment;
// FROM and TO are kilometre points with FROM <= TO. config.Validate uses it
// on Load (a bad segment is a loud error) and the route commands reuse it,
// wrapping the failure as a usage error, so the rule has one single source.
func ParseRouteSegment(segment string) (RouteSegment, error) {
	trimmed := strings.TrimSpace(segment)
	roadPart, pkPart, found := strings.Cut(trimmed, ":")
	roadPart, pkPart = strings.TrimSpace(roadPart), strings.TrimSpace(pkPart)
	if !found || roadPart == "" {
		return RouteSegment{}, fmt.Errorf("segment %q: want \"ROAD:FROM-TO\", for example \"A-7:102-118\"", segment)
	}
	name := road.Normalize(roadPart)
	if name == "" {
		return RouteSegment{}, fmt.Errorf("segment %q: the road name is empty", segment)
	}
	bounds := strings.SplitN(pkPart, "-", 2)
	if len(bounds) != 2 {
		return RouteSegment{}, fmt.Errorf("segment %q: want \"ROAD:FROM-TO\", for example \"A-7:102-118\"", segment)
	}
	from, errFrom := strconv.ParseFloat(strings.TrimSpace(bounds[0]), 64)
	to, errTo := strconv.ParseFloat(strings.TrimSpace(bounds[1]), 64)
	if errFrom != nil || errTo != nil {
		return RouteSegment{}, fmt.Errorf("segment %q: the kilometre points must be numbers, for example \"A-7:102-118\"", segment)
	}
	// strconv also parses "NaN" and "Inf"; a non-finite kilometre point
	// would silently break every range comparison downstream.
	if math.IsNaN(from) || math.IsInf(from, 0) || math.IsNaN(to) || math.IsInf(to, 0) {
		return RouteSegment{}, fmt.Errorf("segment %q: the kilometre points must be finite numbers", segment)
	}
	if from > to {
		return RouteSegment{}, fmt.Errorf("segment %q: the kilometre range is inverted (%g > %g)", segment, from, to)
	}
	return RouteSegment{Road: name, From: from, To: to}, nil
}
