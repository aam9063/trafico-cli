package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
	"github.com/aam9063/trafico-cli/internal/road"
)

// orphanIDLimit caps how many orphan controller ids travel in the stderr
// report before the list is summarised, so a broken feed cannot turn the
// diagnostic into an unbounded line.
const orphanIDLimit = 10

// SignCriteria is the query of the signs command: the shared filters of
// ARRANQUE §5.2 that make sense for panels. --pk, --type and --severity are
// incident concepts and deliberately absent: the sign feed publishes no
// severity or cause, and a single-kilometre-point panel does not span a PK
// range (docs/feeds.md §5).
type SignCriteria struct {
	// Roads matches any listed road compared with road.Equal, so "a54",
	// "A 54" and "A-54" are the same road.
	Roads []string
	// Direction is "", "positive", "negative" or "both"; anything else is
	// a usage error. "both" (and empty) match everything.
	Direction string
	// Provinces are compared with model.ProvinceEqual (case, accents and
	// bilingual pairs fold), so "lugo" matches this feed's "LUGO"
	// (docs/feeds.md §5.1).
	Provinces []string
	// Limit truncates the result; 0 means no limit, negative is a usage
	// error.
	Limit int
}

// SignsResponse is the JSON contract of `trafico signs`: the §5.3 envelope
// with the signs as the payload. Warning carries the stderr notes (cache
// fallback, orphan controllers); it is deliberately not serialised (P-01:
// stdout stays one pure JSON value).
type SignsResponse struct {
	output.Envelope
	// Signs are the matching panels, never null in JSON.
	Signs []model.Sign `json:"signs"`
	// Warning is written to stderr by the CLI layer.
	Warning string `json:"-"`
}

// BuildSigns joins the two sign feeds by id (docs/feeds.md §5):
// vmsControllerReference/@id ↔ ns2:device/@id, the same identifier, no
// heuristic. Devices become signs in feed order; a device without messages
// keeps an empty messages array. A controller whose id matches no device is
// never silently dropped: its id is returned in orphans so the caller can
// report it (stderr channel, P-01). A controller id that appears in more
// than one vmsControllerStatus keeps last-wins (feed order) and is returned
// in duplicates, again so the choice is never silent. Non-vms devices,
// should the DGT ever publish one, are skipped as signs but still count as
// matched ids.
func BuildSigns(devices []datex2.VmsDevice, controllers []datex2.VmsController) ([]model.Sign, []string, []string) {
	messagesByID := make(map[string][]datex2.VmsMessage, len(controllers))
	var duplicates []string
	seenControllers := make(map[string]bool, len(controllers))
	duplicated := make(map[string]bool)
	for _, controller := range controllers {
		if seenControllers[controller.ID] && !duplicated[controller.ID] {
			duplicated[controller.ID] = true
			duplicates = append(duplicates, controller.ID)
		}
		seenControllers[controller.ID] = true
		messagesByID[controller.ID] = controller.Messages
	}
	signs := make([]model.Sign, 0, len(devices))
	matched := make(map[string]bool, len(devices))
	for _, dev := range devices {
		matched[dev.ID] = true
		if dev.TypeOfDevice != "" && dev.TypeOfDevice != "vms" {
			continue
		}
		signs = append(signs, model.SignFromDevice(dev, messagesByID[dev.ID]))
	}
	orphans := []string{}
	seen := map[string]bool{}
	for _, controller := range controllers {
		if !matched[controller.ID] && !seen[controller.ID] {
			seen[controller.ID] = true
			orphans = append(orphans, controller.ID)
		}
	}
	return signs, orphans, duplicates
}

// Signs answers `trafico signs`: it fetches both VMS feeds (FEED-01 owns
// the 60 s cache and the fallback), parses them with datex2, joins them by
// id, applies the shared filters and wraps the matches in the §5.3
// envelope. Invalid criteria are rejected before any request touches the
// network.
func Signs(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, criteria SignCriteria) (SignsResponse, error) {
	if cfg == nil {
		return SignsResponse{}, errs.NewInternal("no hay configuración para consultar los paneles")
	}
	if fetcher == nil {
		return SignsResponse{}, errs.NewInternal("no hay descargador de feeds configurado")
	}
	// Reject invalid combinations before spending a request on the feeds.
	if criteria.Limit < 0 {
		return SignsResponse{}, errs.NewUsage("--limit no puede ser negativo")
	}
	switch criteria.Direction {
	case "", "positive", "negative", "both":
	default:
		return SignsResponse{}, errs.NewUsage(fmt.Sprintf(
			"--direction solo acepta positive, negative o both (recibido %q)", criteria.Direction))
	}

	locationsResult, devices, err := fetchVmsLocations(ctx, fetcher)
	if err != nil {
		return SignsResponse{}, err
	}
	messagesResult, controllers, publishedAt, err := fetchVmsMessages(ctx, fetcher)
	if err != nil {
		return SignsResponse{}, err
	}
	signs, orphans, duplicates := BuildSigns(devices, controllers)
	matches, unlocated := filterSigns(signs, criteria)

	var warnings []string
	for _, result := range []*feeds.Result{locationsResult, messagesResult} {
		if result.Warning != "" {
			warnings = append(warnings, result.Warning)
		}
	}
	if warning := orphanWarning(orphans); warning != "" {
		warnings = append(warnings, warning)
	}
	if warning := duplicateWarning(duplicates); warning != "" {
		warnings = append(warnings, warning)
	}

	meta := signsMeta(locationsResult, messagesResult, publishedAt, fetcher, len(matches), unlocated)
	return SignsResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(messagesResult.FetchedAt)),
		Signs:    matches,
		Warning:  strings.Join(warnings, " "),
	}, nil
}

// fetchVmsLocations downloads and parses the sign-location feed. An
// unparsable body is a feed change (ARRANQUE §6.3: exit 7), never a generic
// internal error.
func fetchVmsLocations(ctx context.Context, fetcher *feeds.Fetcher) (*feeds.Result, []datex2.VmsDevice, error) {
	result, err := fetcher.Fetch(ctx, config.FeedSigns)
	if err != nil {
		return nil, nil, err
	}
	devices, parseErr := datex2.ParseVmsDevices(result.Body())
	closeErr := result.Close()
	switch {
	case parseErr != nil:
		return nil, nil, errs.NewFeedChanged(fmt.Sprintf(
			"el feed %q respondió con un contenido que no se pudo interpretar como XML de localizaciones de paneles: %v",
			config.FeedSigns, parseErr))
	case closeErr != nil:
		return nil, nil, errs.NewInternal(fmt.Sprintf("no se pudo cerrar la lectura del feed %q: %v", config.FeedSigns, closeErr))
	}
	return result, devices, nil
}

// fetchVmsMessages downloads and parses the sign-message feed. The
// publicationTime of the same body is the freshness source of the joined
// output (docs/feeds.md §9), so the body is read once and parsed twice;
// when the feed stops carrying it, publishedAt is zero and the response
// reports published_at null, never an invented instant.
func fetchVmsMessages(ctx context.Context, fetcher *feeds.Fetcher) (*feeds.Result, []datex2.VmsController, time.Time, error) {
	result, err := fetcher.Fetch(ctx, config.FeedSignsMessages)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	body, readErr := io.ReadAll(result.Body())
	closeErr := result.Close()
	switch {
	case readErr != nil:
		return nil, nil, time.Time{}, errs.NewInternal(fmt.Sprintf(
			"no se pudo leer el cuerpo del feed %q: %v", config.FeedSignsMessages, readErr))
	case closeErr != nil:
		return nil, nil, time.Time{}, errs.NewInternal(fmt.Sprintf(
			"no se pudo cerrar la lectura del feed %q: %v", config.FeedSignsMessages, closeErr))
	}
	controllers, parseErr := datex2.ParseVmsMessages(strings.NewReader(string(body)))
	if parseErr != nil {
		return nil, nil, time.Time{}, errs.NewFeedChanged(fmt.Sprintf(
			"el feed %q respondió con un contenido que no se pudo interpretar como XML de mensajes de paneles: %v",
			config.FeedSignsMessages, parseErr))
	}
	publishedAt, timeErr := datex2.ParseVmsPublicationTime(strings.NewReader(string(body)))
	if timeErr != nil {
		return nil, nil, time.Time{}, errs.NewFeedChanged(fmt.Sprintf(
			"el feed %q trae una com:publicationTime que no se pudo interpretar: %v",
			config.FeedSignsMessages, timeErr))
	}
	return result, controllers, publishedAt, nil
}

// filterSigns applies the criteria that make sense for panels. A sign with
// no province only counts as unlocated when a province filter is set: it
// would have matched but cannot be placed (the same meta.unlocated contract
// as the incidents command).
func filterSigns(signs []model.Sign, criteria SignCriteria) ([]model.Sign, int) {
	matches := make([]model.Sign, 0, len(signs))
	unlocated := 0
	for _, sign := range signs {
		if !signRoadMatches(sign, criteria.Roads) {
			continue
		}
		if !signDirectionMatches(sign, criteria.Direction) {
			continue
		}
		if len(criteria.Provinces) > 0 {
			if sign.Province == "" {
				unlocated++
				continue
			}
			if !signProvinceMatches(sign, criteria.Provinces) {
				continue
			}
		}
		matches = append(matches, sign)
	}
	if criteria.Limit > 0 && len(matches) > criteria.Limit {
		matches = matches[:criteria.Limit]
	}
	return matches, unlocated
}

func signRoadMatches(sign model.Sign, roads []string) bool {
	if len(roads) == 0 {
		return true
	}
	for _, wanted := range roads {
		if road.Equal(sign.Road, wanted) {
			return true
		}
	}
	return false
}

// signDirectionMatches mirrors the direction semantics of internal/filter
// (docs/feeds.md §4.4): "both" means no filter, a sign travelling "both"
// directions matches a one-way filter, and a sign with no published
// direction only matches when no direction filter is set.
func signDirectionMatches(sign model.Sign, direction string) bool {
	switch direction {
	case "", "both":
		return true
	case "positive", "negative":
		if sign.Direction == nil {
			return false
		}
		return *sign.Direction == direction || *sign.Direction == "both"
	default:
		return true
	}
}

func signProvinceMatches(sign model.Sign, provinces []string) bool {
	for _, wanted := range provinces {
		if model.ProvinceEqual(sign.Province, wanted) {
			return true
		}
	}
	return false
}

// orphanWarning renders the stderr report of orphan controllers: messages
// the DGT publishes for ids that no device carries, so they are never
// silently dropped (P-01: stderr, never stdout).
func orphanWarning(orphans []string) string {
	if len(orphans) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"aviso: %d mensajes de panel no tienen ningún dispositivo con el mismo id en el feed de localizaciones y no se muestran: %s.",
		len(orphans), idList(orphans))
}

// duplicateWarning renders the stderr report of duplicated controller ids
// (review FEED-03): the messages feed carried the same id in more than one
// vmsControllerStatus, and only the payload's last block is shown, so the
// resolution is never silent (P-01: stderr, never stdout).
func duplicateWarning(duplicates []string) string {
	if len(duplicates) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"aviso: %d controladores de panel aparecen duplicados en el feed de mensajes y solo se muestra su último bloque: %s.",
		len(duplicates), idList(duplicates))
}

// idList renders the id detail of a stderr report, capping how many ids
// travel in it before the list is summarised, so a broken feed cannot turn
// the diagnostic into an unbounded line.
func idList(ids []string) string {
	shown := ids
	extra := 0
	if len(ids) > orphanIDLimit {
		shown = ids[:orphanIDLimit]
		extra = len(ids) - orphanIDLimit
	}
	detail := strings.Join(shown, ", ")
	if extra > 0 {
		detail = fmt.Sprintf("%s y %d más", detail, extra)
	}
	return detail
}

// signsMeta builds the meta block of the signs envelope. The messages feed
// is the freshness source (the locations feed is quasi-static, docs/feeds.md
// §5.1): published_at comes from its publicationTime, age_seconds derives
// from it (never negative), and from_cache and stale reflect both feeds.
// FetchedAt is the messages download, the content that changes.
func signsMeta(locationsResult, messagesResult *feeds.Result, publishedAt time.Time, fetcher *feeds.Fetcher, total, unlocated int) *output.Meta {
	freshness := messagesResult.Freshness(&publishedAt, fetcher.MaxAge, fetcher.Now())
	meta := &output.Meta{
		Feed:        config.FeedSignsMessages,
		FetchedAt:   messagesResult.FetchedAt.UTC(),
		PublishedAt: utcTimePtr(freshness.PublishedAt),
		AgeSeconds:  freshness.AgeSeconds,
		Stale:       freshness.Stale || locationsResult.Stale || messagesResult.Stale,
		FromCache:   locationsResult.FromCache || messagesResult.FromCache,
		Total:       total,
		Unlocated:   unlocated,
	}
	if meta.AgeSeconds != nil && *meta.AgeSeconds < 0 {
		age := int64(0)
		meta.AgeSeconds = &age
	}
	return meta
}
