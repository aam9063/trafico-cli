package feeds

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientDefaultTimeout(t *testing.T) {
	client := NewHTTPClient("test")
	if client.HTTP.Timeout != 30*time.Second {
		t.Fatalf("default HTTP timeout = %v, want 30s", client.HTTP.Timeout)
	}
}

// A polite client identifies itself and never sends conditional headers: the
// measured feeds return neither ETag nor Last-Modified (docs/feeds.md §2), so
// If-Modified-Since / If-None-Match would be dead code.
func TestHTTPClientSendsUserAgentAndNoConditionalHeaders(t *testing.T) {
	var gotUserAgent, gotIfModifiedSince, gotIfNoneMatch, gotAcceptEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		gotIfModifiedSince = r.Header.Get("If-Modified-Since")
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		_, _ = w.Write([]byte("<feed/>"))
	}))
	defer srv.Close()

	resp, err := NewHTTPClient("test").Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("reading body: %v", err)
	}

	if gotUserAgent != "trafico/test" {
		t.Errorf("User-Agent = %q, want %q", gotUserAgent, "trafico/test")
	}
	if gotIfModifiedSince != "" {
		t.Errorf("If-Modified-Since = %q, want no conditional header", gotIfModifiedSince)
	}
	if gotIfNoneMatch != "" {
		t.Errorf("If-None-Match = %q, want no conditional header", gotIfNoneMatch)
	}
	if !strings.Contains(gotAcceptEncoding, "gzip") {
		t.Errorf("Accept-Encoding = %q, want gzip negotiated", gotAcceptEncoding)
	}
}

// The feeds send gzip (7.17 MB plain -> 224 KB on the wire, docs/feeds.md §2);
// Go's transport must negotiate it and decompress transparently.
func TestHTTPClientDecodesGzip(t *testing.T) {
	want := "<d2LogicalModel>hola ñ y acentos</d2LogicalModel>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(want))
		_ = gz.Close()
	}))
	defer srv.Close()

	resp, err := NewHTTPClient("test").Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(got) != want {
		t.Errorf("decoded body = %q, want %q", got, want)
	}
}
