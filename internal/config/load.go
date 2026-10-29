package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"

	"github.com/pelletier/go-toml/v2"
)

// rawConfig mirrors Config with optional fields so Load can tell "the file
// set this" from "the file left this at its zero value", which the merge over
// defaults needs (an explicit enabled = false must not restore the default
// true, and an absent enabled must keep it).
type rawConfig struct {
	Feeds  map[string]rawFeed  `toml:"feeds"`
	Places map[string]string   `toml:"places"`
	Routes map[string]rawRoute `toml:"routes"`
	Stale  *rawStale           `toml:"stale"`
	Sinks  map[string]rawSink  `toml:"sinks"`
}

// rawRoute mirrors Route with optional fields so the merge can tell "the
// file set this" from "the file left this at its zero value" (a missing
// segments key must land as a route with no segments, which Validate then
// rejects loudly).
type rawRoute struct {
	Direction *string   `toml:"direction"`
	Segments  *[]string `toml:"segments"`
}

type rawFeed struct {
	URL     *string `toml:"url"`
	Enabled *bool   `toml:"enabled"`
}

type rawStale struct {
	MaxAge *Duration `toml:"max_age"`
}

type rawSink struct {
	Type     *string `toml:"type"`
	URL      *string `toml:"url"`
	Topic    *string `toml:"topic"`
	Template *string `toml:"template"`
	ChatID   *string `toml:"chat_id"`
}

// Load reads the TOML configuration at path and merges it over the compiled
// defaults, so a file that only overrides one feed URL keeps the other six.
//
// A missing file is not an error: the defaults are returned so the tool works
// out of the box (P-03). A file whose permissions are wider than 0600 is a
// loud error, because the config holds personal locations that must never be
// group or world readable (P-07); the check does not apply on Windows, where
// POSIX permission bits do not exist and access is governed by ACLs.
func Load(path string) (*Config, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: cannot read %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("config: %s is a directory, want a TOML file", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("config: %s has permissions %04o, want 0600: the config holds personal locations and must not be group or world readable (run: chmod 600 %s)", path, info.Mode().Perm(), path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: cannot read %s: %w", path, err)
	}

	cfg, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("config: %s is not a valid configuration: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %s is invalid: %w", path, err)
	}
	return cfg, nil
}

// decode parses strict TOML and merges it over the compiled defaults.
// Unknown keys are rejected because a typo silently ignored is a config bug
// the user cannot see.
func decode(data []byte) (*Config, error) {
	var raw rawConfig
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}

	cfg := Default()

	for name, feed := range raw.Feeds {
		merged := Feed{URL: cfg.Feeds[name].URL, Enabled: cfg.Feeds[name].Enabled}
		if feed.URL != nil {
			merged.URL = *feed.URL
		}
		if feed.Enabled != nil {
			merged.Enabled = *feed.Enabled
		}
		cfg.Feeds[name] = merged
	}

	for name, value := range raw.Places {
		cfg.Places[name] = value
	}

	for name, route := range raw.Routes {
		merged := Route{}
		if route.Direction != nil {
			merged.Direction = *route.Direction
		}
		if route.Segments != nil {
			merged.Segments = *route.Segments
		}
		cfg.Routes[name] = merged
	}

	if raw.Stale != nil && raw.Stale.MaxAge != nil {
		cfg.Stale.MaxAge = *raw.Stale.MaxAge
	}

	for name, sink := range raw.Sinks {
		merged := cfg.Sinks[name]
		if sink.Type != nil {
			merged.Type = *sink.Type
		}
		if sink.URL != nil {
			merged.URL = *sink.URL
		}
		if sink.Topic != nil {
			merged.Topic = *sink.Topic
		}
		if sink.Template != nil {
			merged.Template = *sink.Template
		}
		if sink.ChatID != nil {
			merged.ChatID = *sink.ChatID
		}
		cfg.Sinks[name] = merged
	}

	return cfg, nil
}
