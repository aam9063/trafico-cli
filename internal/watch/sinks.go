package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/store"
)

// Sink is one notification destination of docs/ARRANQUE.md §5.5 and spec S8:
// ntfy, a webhook (discord, slack, telegram templates) or a local command.
// A sink receives only the rendered message text (message.go), which by
// construction carries no watch name, route name, place name or coordinate.
type Sink interface {
	// Deliver sends one message. A non-nil error means the delivery failed
	// and the event must stay undelivered for retry.
	Deliver(ctx context.Context, message string) error
}

// defaultHTTPTimeout bounds every sink request; a notification that takes
// longer is a failed delivery, not a hung check.
const defaultHTTPTimeout = 10 * time.Second

// httpClientOf returns the sink's client or a shared default with a timeout.
func httpClientOf(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

// checkStatus closes the response body on every path and turns a non-2xx
// response into a delivery failure.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Drain the body so the underlying connection goes back to the
		// pool, then close it: a sink that runs every five minutes must
		// not leak one connection per successful notification.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return nil
	}
	// Drain a bounded slice of the body for the error context, then close.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	detail := strings.TrimSpace(string(body))
	if detail != "" {
		return fmt.Errorf("el destino de avisos respondió con el estado HTTP %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("el destino de avisos respondió con el estado HTTP %d", resp.StatusCode)
}

// NtfySink POSTs the message to <url>/<topic> (spec S8), the shape ntfy.sh
// and self-hosted ntfy servers accept for a plain text notification.
type NtfySink struct {
	// BaseURL is the server base, for example https://ntfy.sh (trailing
	// slashes are tolerated).
	BaseURL string
	// Topic is the ntfy topic to publish on.
	Topic string
	// Client is optional; nil uses a default client with a timeout.
	Client *http.Client
}

// Deliver implements Sink.
func (s *NtfySink) Deliver(ctx context.Context, message string) error {
	if s.Topic == "" {
		return fmt.Errorf("el destino ntfy no tiene topic configurado")
	}
	url := strings.TrimRight(s.BaseURL, "/") + "/" + s.Topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("no se pudo preparar el aviso para ntfy: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := httpClientOf(s.Client).Do(req)
	if err != nil {
		return fmt.Errorf("no se pudo enviar el aviso a ntfy: %w", err)
	}
	return checkStatus(resp)
}

// WebhookStyle selects the body template of a WebhookSink (spec S8).
type WebhookStyle string

// The webhook templates of spec S8. Discord and Slack take a text payload;
// Telegram needs the destination chat.
const (
	StyleDiscord  WebhookStyle = "discord"
	StyleSlack    WebhookStyle = "slack"
	StyleTelegram WebhookStyle = "telegram"
)

// WebhookSink POSTs the message as JSON to a webhook URL, shaped by its
// template style. The message is user content and always travels as a JSON
// string value, never interpolated into the template.
type WebhookSink struct {
	// URL is the webhook endpoint.
	URL string
	// Style selects the body template.
	Style WebhookStyle
	// ChatID is the Telegram destination chat; required for
	// StyleTelegram, ignored otherwise.
	ChatID string
	// Client is optional; nil uses a default client with a timeout.
	Client *http.Client
}

// Deliver implements Sink.
func (s *WebhookSink) Deliver(ctx context.Context, message string) error {
	var body any
	switch s.Style {
	case StyleDiscord:
		body = map[string]string{"content": message}
	case StyleSlack:
		body = map[string]string{"text": message}
	case StyleTelegram:
		if s.ChatID == "" {
			return fmt.Errorf("el destino de telegram necesita un chat_id")
		}
		body = map[string]string{"chat_id": s.ChatID, "text": message}
	default:
		return fmt.Errorf("estilo de webhook desconocido %q: usa discord, slack o telegram", s.Style)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("no se pudo serializar el cuerpo del webhook: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("no se pudo preparar el aviso para el webhook: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClientOf(s.Client).Do(req)
	if err != nil {
		return fmt.Errorf("no se pudo enviar el aviso al webhook: %w", err)
	}
	return checkStatus(resp)
}

// ExecSink runs a local command with the message on stdin (spec S8). The
// command and its arguments come from the user's own configuration, so they
// are trusted input; the message is passed through stdin, never through the
// shell, so no quoting can turn it into something else.
type ExecSink struct {
	// Command is the executable to run.
	Command string
	// Args are the arguments of the command, without the executable.
	Args []string
}

// Deliver implements Sink: a non-zero exit (or a failed start) is a delivery
// failure, not a panic, and leaves the event undelivered for retry.
func (s *ExecSink) Deliver(ctx context.Context, message string) error {
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	cmd.Stdin = strings.NewReader(message)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("el comando de avisos terminó con error (%v): %s", err, detail)
		}
		return fmt.Errorf("el comando de avisos terminó con error: %w", err)
	}
	return nil
}

// DeliverOne renders the event payload and delivers it through the sink. On
// success the event is stamped delivered (store.MarkDelivered); on any
// failure — render, request, non-2xx, non-zero exec exit — the event stays
// undelivered in the log so a later run can retry it (spec S8).
func DeliverOne(ctx context.Context, st *store.Store, event store.Event, sink Sink) error {
	message, err := RenderMessage(event.Payload)
	if err != nil {
		return err
	}
	if err := sink.Deliver(ctx, message); err != nil {
		return err
	}
	return st.MarkDelivered([]int64{event.ID})
}

// DrainPending delivers the pending events oldest first (the store's retry
// order). The two failure kinds get the treatment each of them deserves:
//
//   - A sink.Deliver failure (network, non-2xx, failing command) is
//     transient: the drain stops there and the event stays queued together
//     with everything behind it for the next retry.
//   - A payload that cannot be rendered (RenderMessage error) is
//     undeliverable data, not an outage: retrying can never succeed, so it
//     is stamped out of the retry queue (store.MarkSkipped; its row stays in
//     the log for diagnostics) and the drain continues with the rest. One
//     bad event must never block every notification behind it forever.
//
// limit <= 0 means no limit. It returns how many events were delivered
// (skipped ones are not counted).
func DrainPending(ctx context.Context, st *store.Store, sink Sink, limit int) (int, error) {
	events, err := st.UndeliveredEvents(limit)
	if err != nil {
		return 0, err
	}
	delivered := 0
	for _, event := range events {
		message, err := RenderMessage(event.Payload)
		if err != nil {
			if markErr := st.MarkSkipped([]int64{event.ID}); markErr != nil {
				return delivered, markErr
			}
			continue
		}
		if err := sink.Deliver(ctx, message); err != nil {
			return delivered, err
		}
		if err := st.MarkDelivered([]int64{event.ID}); err != nil {
			return delivered, err
		}
		delivered++
	}
	return delivered, nil
}
