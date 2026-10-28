package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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
