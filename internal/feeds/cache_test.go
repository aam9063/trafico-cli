package feeds

import (
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCacheStoreAndLoadRoundTrip(t *testing.T) {
	cache := NewCache(t.TempDir())
	now := time.Now().Truncate(time.Second)
	wantURL := "https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml"

	entry, err := cache.Store("dgt-situations", wantURL, strings.NewReader("cuerpo con ñ"), now)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if entry.FetchedAt != now {
		t.Errorf("entry.FetchedAt = %v, want %v", entry.FetchedAt, now)
	}
	if entry.Size != int64(len("cuerpo con ñ")) {
		t.Errorf("entry.Size = %d, want %d", entry.Size, len("cuerpo con ñ"))
	}
	if entry.SourceURL != wantURL {
		t.Errorf("entry.SourceURL = %q, want %q", entry.SourceURL, wantURL)
	}

	body, got, err := cache.Load("dgt-situations")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading cached body: %v", err)
	}
	if string(data) != "cuerpo con ñ" {
		t.Errorf("cached body = %q, want %q", data, "cuerpo con ñ")
	}
	if !got.FetchedAt.Equal(now) {
		t.Errorf("loaded FetchedAt = %v, want %v", got.FetchedAt, now)
	}
	if got.SourceURL != wantURL {
		t.Errorf("loaded SourceURL = %q, want %q", got.SourceURL, wantURL)
	}
	if got.Size != int64(len("cuerpo con ñ")) {
		t.Errorf("loaded Size = %d, want %d", got.Size, len("cuerpo con ñ"))
	}
}

// P-06: the shared cache lasts 60 seconds.
func TestCacheFreshAndAge(t *testing.T) {
	cache := NewCache(t.TempDir())
	fetchedAt := time.Now()

	entry, err := cache.Store("dgt-situations", "https://example.test/feed.xml", strings.NewReader("x"), fetchedAt)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	if age := cache.Age(entry, fetchedAt.Add(59*time.Second)); age != 59*time.Second {
		t.Errorf("Age after 59s = %v, want 59s", age)
	}
	if !cache.Fresh(entry, fetchedAt.Add(59*time.Second)) {
		t.Error("entry 59s old should still be fresh (TTL is 60s)")
	}
	if cache.Fresh(entry, fetchedAt.Add(61*time.Second)) {
		t.Error("entry 61s old should be expired (TTL is 60s)")
	}
	if age := cache.Age(entry, fetchedAt.Add(61*time.Second)); age != 61*time.Second {
		t.Errorf("Age after 61s = %v, want 61s", age)
	}
}

// The cache holds feed snapshots alongside personal-adjacent local state, so
// the file must be private (0600).
func TestCacheFilePermissionsAre0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not exist on Windows; CI runs Linux and macOS, where this check applies")
	}
	cache := NewCache(t.TempDir())
	if _, err := cache.Store("dgt-situations", "https://example.test/feed.xml", strings.NewReader("x"), time.Now()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	path, err := cache.path("dgt-situations")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("cache file mode = %v, want 0600", got)
	}
}

func TestCacheLoadMissingEntryFails(t *testing.T) {
	cache := NewCache(t.TempDir())
	if _, _, err := cache.Load("dgt-situations"); err == nil {
		t.Fatal("Load of a missing entry should fail")
	}
}

func TestCacheRejectsUnsafeName(t *testing.T) {
	cache := NewCache(t.TempDir())
	for _, name := range []string{"../evil", "", "sub/dir"} {
		if _, err := cache.Store(name, "https://example.test/feed.xml", strings.NewReader("x"), time.Now()); err == nil {
			t.Errorf("Store with unsafe name %q should fail", name)
		}
	}
}
