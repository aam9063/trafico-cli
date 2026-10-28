package feeds

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// ErrBodyRead reports that reading the *source* body failed. io.Copy returns
// only the first error and it may come from either side, so Store tracks the
// read side separately: this sentinel is a transport failure the caller must
// treat as a network error, never as a local disk problem.
var ErrBodyRead = errors.New("feeds: reading the source body failed")

// ErrEmptyBody reports a source that produced zero bytes. An empty DATEX II
// publication is not a valid document, and it must never reach the cache: a
// poisoned entry would be served as a successful download on the next call.
var ErrEmptyBody = errors.New("feeds: the source body is empty")

// bodyReader records a read-side failure so Store can tell a transport failure
// from a local write failure.
type bodyReader struct {
	r   io.Reader
	err error
}

func (b *bodyReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		b.err = err
	}
	return n, err
}

// DefaultTTL is the duration of the shared 60 s cache (P-06): the feed changes
// every minute, so two commands in a row must reuse one download, neither more
// nor less often.
const DefaultTTL = 60 * time.Second

// headerVersion is the version of the cache file format, bumped when the
// header shape changes so old entries read as corrupt and are overwritten.
const headerVersion = 1

// headerLine is the first line of every cache file: one JSON object, a
// newline, and then the raw feed body. Keeping header and body in a single
// file written atomically means a crash can never pair a new body with old
// metadata, nor leave a truncated entry behind.
type headerLine struct {
	Version   int       `json:"v"`
	FetchedAt time.Time `json:"fetched_at"`
	SourceURL string    `json:"url"`
}

// Entry is the metadata of one cached feed download: everything needed to
// answer freshness and "feeds status" questions without the network.
type Entry struct {
	// FetchedAt is when the body was downloaded from the source.
	FetchedAt time.Time
	// Size is the size of the raw body in bytes (without the header line).
	Size int64
	// SourceURL is the URL the body was downloaded from.
	SourceURL string
}

// Cache is the shared on-disk feed cache under config.Paths.CacheDir: one
// entry per feed name, each a private 0600 file.
type Cache struct {
	// Dir is the directory holding the cache entries.
	Dir string
	// TTL is how long an entry stays fresh (P-06); zero means DefaultTTL.
	TTL time.Duration
}

// NewCache returns a cache under dir with the default 60 s TTL.
func NewCache(dir string) *Cache {
	return &Cache{Dir: dir, TTL: DefaultTTL}
}

// feedNameRE accepts the feed names of internal/config and rejects anything
// that could escape the cache directory or be empty.
var feedNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func (c *Cache) path(name string) (string, error) {
	if !feedNameRE.MatchString(name) {
		return "", fmt.Errorf("feeds: invalid cache entry name %q", name)
	}
	return filepath.Join(c.Dir, name+".cache"), nil
}

// Age returns how long ago the entry was downloaded, relative to now. A clock
// that jumps backwards would otherwise report a negative age, which Fresh
// would read as very fresh: clamp at zero so a future-dated entry is simply
// not old yet, never fresher than a fresh download.
func (c *Cache) Age(entry Entry, now time.Time) time.Duration {
	age := now.Sub(entry.FetchedAt)
	if age < 0 {
		return 0
	}
	return age
}

// Fresh reports whether the entry is still inside the 60 s shared cache TTL
// (P-06) at time now. An entry dated in the future — a clock that jumped
// backwards — is never fresh: it cannot be trusted, so it is refetched.
func (c *Cache) Fresh(entry Entry, now time.Time) bool {
	age := now.Sub(entry.FetchedAt)
	if age < 0 {
		return false
	}
	return age < c.ttl()
}

func (c *Cache) ttl() time.Duration {
	if c.TTL <= 0 {
		return DefaultTTL
	}
	return c.TTL
}

// Store streams body into the cache entry for name atomically: the bytes go to
// a temporary file in the same directory, are flushed with fsync, get mode
// 0600 (the cache holds feed snapshots and must be private) and are renamed
// over the target, so a crash mid-write leaves either the old entry or the new
// one, never a truncated one.
func (c *Cache) Store(name, sourceURL string, body io.Reader, fetchedAt time.Time) (Entry, error) {
	path, err := c.path(name)
	if err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return Entry{}, fmt.Errorf("feeds: cannot create cache directory %s: %w", c.Dir, err)
	}

	tmp, err := os.CreateTemp(c.Dir, "."+name+".*.tmp")
	if err != nil {
		return Entry{}, fmt.Errorf("feeds: cannot create temporary file for %s: %w", name, err)
	}
	tmpName := tmp.Name()
	// Best effort: after a successful rename the file no longer exists.
	defer os.Remove(tmpName)

	header, err := json.Marshal(headerLine{Version: headerVersion, FetchedAt: fetchedAt, SourceURL: sourceURL})
	if err != nil {
		tmp.Close()
		return Entry{}, fmt.Errorf("feeds: cannot serialise cache header for %s: %w", name, err)
	}
	if _, err := tmp.Write(append(header, '\n')); err != nil {
		tmp.Close()
		return Entry{}, fmt.Errorf("feeds: cannot write cache header for %s: %w", name, err)
	}
	tracked := &bodyReader{r: body}
	size, err := io.Copy(tmp, tracked)
	if err != nil {
		tmp.Close()
		if tracked.err != nil {
			return Entry{}, fmt.Errorf("%w: %v", ErrBodyRead, tracked.err)
		}
		return Entry{}, fmt.Errorf("feeds: cannot write cache body for %s: %w", name, err)
	}
	if size == 0 {
		tmp.Close()
		return Entry{}, ErrEmptyBody
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return Entry{}, fmt.Errorf("feeds: cannot flush cache entry for %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return Entry{}, fmt.Errorf("feeds: cannot close cache entry for %s: %w", name, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return Entry{}, fmt.Errorf("feeds: cannot set permissions on the cache entry for %s: %w", name, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return Entry{}, fmt.Errorf("feeds: cannot install cache entry for %s: %w", name, err)
	}
	return Entry{FetchedAt: fetchedAt, Size: size, SourceURL: sourceURL}, nil
}

// Load returns a reader over the cached body of name (without the header
// line) together with its metadata. The caller must Close the reader. Any
// error (missing, truncated or corrupt entry) is a cache miss for the caller.
func (c *Cache) Load(name string) (io.ReadCloser, Entry, error) {
	path, err := c.path(name)
	if err != nil {
		return nil, Entry{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, Entry{}, fmt.Errorf("feeds: cannot read cache entry %q: %w", name, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, Entry{}, fmt.Errorf("feeds: cannot stat cache entry %q: %w", name, err)
	}

	buffered := bufio.NewReader(file)
	line, err := buffered.ReadBytes('\n')
	if err != nil {
		file.Close()
		return nil, Entry{}, fmt.Errorf("feeds: cache entry %q is truncated", name)
	}
	var header headerLine
	if err := json.Unmarshal(line, &header); err != nil || header.Version != headerVersion {
		file.Close()
		return nil, Entry{}, fmt.Errorf("feeds: cache entry %q has an unexpected header", name)
	}

	entry := Entry{
		FetchedAt: header.FetchedAt,
		SourceURL: header.SourceURL,
		Size:      info.Size() - int64(len(line)),
	}
	return &cacheBody{file: file, buffered: buffered}, entry, nil
}

// cacheBody streams the body portion of a cache file: it reads through the
// bufio.Reader that already consumed the header line and closes the file.
type cacheBody struct {
	file     *os.File
	buffered *bufio.Reader
}

func (b *cacheBody) Read(p []byte) (int, error) { return b.buffered.Read(p) }

func (b *cacheBody) Close() error { return b.file.Close() }
