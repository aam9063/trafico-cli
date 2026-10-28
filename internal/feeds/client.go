package feeds

import (
	"context"
	"net/http"
	"time"
)

// DefaultTimeout is the default timeout of one feed download, covering the
// whole exchange (connection, headers and body).
const DefaultTimeout = 30 * time.Second

// HTTPClient performs the polite HTTP GETs of the DGT feeds (P-06): it
// identifies itself with a User-Agent, lets Go's transport negotiate gzip and
// hands the response body to the caller as a stream, so the multi-megabyte
// feeds are never read whole into memory.
//
// internal/feeds is the only package that performs HTTP (docs/ARRANQUE.md
// §8.2).
type HTTPClient struct {
	// HTTP is the underlying client. Its Timeout applies to the request
	// including reading the body; zero means DefaultTimeout was replaced and
	// is the caller's choice.
	HTTP *http.Client
	// UserAgent names the tool and its version ("trafico/<version>"): a
	// polite client identifies itself to the DGT.
	UserAgent string
}

// NewHTTPClient returns a client with an explicit 30 s timeout (DefaultTimeout)
// and the User-Agent "trafico/<version>".
func NewHTTPClient(version string) *HTTPClient {
	return &HTTPClient{
		HTTP:      &http.Client{Timeout: DefaultTimeout},
		UserAgent: "trafico/" + version,
	}
}

// Get performs one streaming GET of url. The caller reads and closes
// resp.Body. Go's transport adds "Accept-Encoding: gzip" itself and
// transparently decompresses the response, which is what docs/feeds.md §2
// recommends (7.17 MB plain -> 224 KB on the wire).
//
// No conditional headers are sent on purpose: the measured feeds return
// neither ETag nor Last-Modified (docs/feeds.md §2), so If-None-Match and
// If-Modified-Since would be dead code that can never produce a 304. Politeness
// comes from the shared 60 s cache instead.
func (c *HTTPClient) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	return c.HTTP.Do(req)
}
