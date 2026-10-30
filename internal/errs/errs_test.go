package errs

import (
	"bytes"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// TestConstructorsWriteExactEnvelopeAndExitCode is table-driven over every
// constructor of ARRANQUE section 6.3: exact JSON envelope (one line, exact
// key order) and the exit code ExitCodeFor reports.
func TestConstructorsWriteExactEnvelopeAndExitCode(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		wantJSON string
		wantExit ExitCode
	}{
		{
			name:     "internal",
			err:      NewInternal("pánico inesperado"),
			wantJSON: `{"error":{"type":"internal","message":"pánico inesperado","exit_code":1}}`,
			wantExit: ExitInternal,
		},
		{
			name:     "usage",
			err:      NewUsage("--pk sin --road"),
			wantJSON: `{"error":{"type":"usage","message":"--pk sin --road","exit_code":2}}`,
			wantExit: ExitUsage,
		},
		{
			name:     "cancelled",
			err:      NewCancelled("cancelado por el usuario"),
			wantJSON: `{"error":{"type":"cancelled","message":"cancelado por el usuario","exit_code":2}}`,
			wantExit: ExitUsage,
		},
		{
			name:     "confirmation_required",
			err:      NewConfirmationRequired("falta --yes para borrar"),
			wantJSON: `{"error":{"type":"confirmation_required","message":"falta --yes para borrar","exit_code":2}}`,
			wantExit: ExitUsage,
		},
		{
			name:     "not_found",
			err:      NewNotFound("la ruta fija no existe"),
			wantJSON: `{"error":{"type":"not_found","message":"la ruta fija no existe","exit_code":4}}`,
			wantExit: ExitNotFound,
		},
		{
			name:     "rate_limited",
			err:      NewRateLimited("la DGT respondió 429", "dgt-situations"),
			wantJSON: `{"error":{"type":"rate_limited","message":"la DGT respondió 429","exit_code":5,"feed":"dgt-situations"}}`,
			wantExit: ExitUpstream,
		},
		{
			name:     "upstream_unavailable",
			err:      NewUpstreamUnavailable("la DGT respondió 503", "dgt-situations"),
			wantJSON: `{"error":{"type":"upstream_unavailable","message":"la DGT respondió 503","exit_code":5,"feed":"dgt-situations"}}`,
			wantExit: ExitUpstream,
		},
		{
			name:     "network",
			err:      NewNetwork("sin conexión de red"),
			wantJSON: `{"error":{"type":"network","message":"sin conexión de red","exit_code":6}}`,
			wantExit: ExitNetwork,
		},
		{
			name:     "feed_unavailable",
			err:      NewFeedUnavailable("La DGT no responde y no hay caché local.", "dgt-situations"),
			wantJSON: `{"error":{"type":"feed_unavailable","message":"La DGT no responde y no hay caché local.","exit_code":6,"feed":"dgt-situations"}}`,
			wantExit: ExitNetwork,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteError(&buf, tc.err); err != nil {
				t.Fatalf("WriteError failed: %v", err)
			}
			if got := buf.String(); got != tc.wantJSON+"\n" {
				t.Fatalf("envelope = %q, want %q", got, tc.wantJSON+"\n")
			}
			if got := ExitCodeFor(tc.err); got != tc.wantExit {
				t.Fatalf("ExitCodeFor = %d, want %d", got, tc.wantExit)
			}
			if tc.err.Type != tc.name {
				t.Fatalf("Type = %q, want %q", tc.err.Type, tc.name)
			}
		})
	}
}

func TestWriteErrorOmitsFeedKeyWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteError(&buf, NewNotFound("no existe")); err != nil {
		t.Fatalf("WriteError failed: %v", err)
	}
	if got := buf.String(); strings.Contains(got, `"feed"`) {
		t.Fatalf("envelope contains feed key without a feed set: %q", got)
	}
	if lines := strings.Count(buf.String(), "\n"); lines != 1 {
		t.Fatalf("envelope must be exactly one line, got %d newlines", lines)
	}
}

func TestWriteErrorPlainErrorMapsToInternal(t *testing.T) {
	plain := errors.New("algo se rompió")
	var buf bytes.Buffer
	if err := WriteError(&buf, plain); err != nil {
		t.Fatalf("WriteError failed: %v", err)
	}
	want := `{"error":{"type":"internal","message":"algo se rompió","exit_code":1}}` + "\n"
	if got := buf.String(); got != want {
		t.Fatalf("envelope = %q, want %q", got, want)
	}
	if got := ExitCodeFor(plain); got != ExitInternal {
		t.Fatalf("ExitCodeFor(plain) = %d, want %d", got, ExitInternal)
	}
}

func TestWriteErrorNilIsNoop(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteError(&buf, nil); err != nil {
		t.Fatalf("WriteError(nil) failed: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("WriteError(nil) wrote %q", buf.String())
	}
}

func TestFeedChangedMessageEmbedsEscapedIssueURL(t *testing.T) {
	err := NewFeedChanged("la URL del feed respondió 404")
	var buf bytes.Buffer
	if werr := WriteError(&buf, err); werr != nil {
		t.Fatalf("WriteError failed: %v", werr)
	}
	if got := ExitCodeFor(err); got != ExitFeedChanged {
		t.Fatalf("ExitCodeFor = %d, want %d", got, ExitFeedChanged)
	}
	if err.Type != "feed_changed" {
		t.Fatalf("Type = %q, want %q", err.Type, "feed_changed")
	}
	const base = "https://github.com/aam9063/trafico-cli/issues/new?"
	idx := strings.Index(err.Message, base)
	if idx < 0 {
		t.Fatalf("message does not contain an issues/new URL: %q", err.Message)
	}
	rawQuery := err.Message[idx+len(base):]
	parsed, perr := url.Parse("?" + rawQuery)
	if perr != nil {
		t.Fatalf("issue URL query does not parse: %v (raw %q)", perr, rawQuery)
	}
	q := parsed.Query()
	if got := q.Get("title"); got != "La DGT ha cambiado el feed" {
		t.Fatalf("issue title = %q, want %q", got, "La DGT ha cambiado el feed")
	}
	if strings.Contains(rawQuery, " ") {
		t.Fatalf("issue URL query contains unescaped spaces: %q", rawQuery)
	}
	if got := q.Get("body"); !strings.Contains(got, "404") {
		t.Fatalf("issue body = %q, want it to contain the detail", got)
	}
}
