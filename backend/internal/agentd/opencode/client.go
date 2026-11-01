package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultRequestTimeout bounds non-streaming calls so a wedged opencode
	// process cannot stall the agent forever. Callers that need otherwise
	// should pass an *http.Client carrying their own timeout.
	defaultRequestTimeout = 30 * time.Second

	// maxErrorBody caps how much of a failed response is echoed into an error.
	// Bodies are truncated because an error string ends up in logs.
	maxErrorBody = 4 << 10

	// eventStreamBuffer lets the SSE reader stay ahead of a slow consumer for
	// short bursts without blocking the scan loop.
	eventStreamBuffer = 64

	// maxEventSize is the largest single SSE line accepted. Tool results are
	// pre-truncated server-side, so this is a safety valve rather than a limit.
	maxEventSize = 4 << 20
)

// Client talks to a local opencode server. It is stateless beyond its
// endpoints and safe for concurrent use.
type Client struct {
	baseURL string
	// httpClient carries a timeout; it is used for request/response calls.
	httpClient *http.Client
	// streamClient is the same transport with no overall timeout, because the
	// SSE feed is long-lived and is instead bounded by its context.
	streamClient *http.Client
}

// New builds a client for baseURL (for example "http://127.0.0.1:4096").
// A nil httpClient selects the default transport, and a client without a
// timeout gets defaultRequestTimeout for non-streaming requests.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	requestClient := *httpClient
	if requestClient.Timeout == 0 {
		requestClient.Timeout = defaultRequestTimeout
	}
	streamClient := *httpClient
	streamClient.Timeout = 0

	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		httpClient:   &requestClient,
		streamClient: &streamClient,
	}
}

// Health reports whether the server accepts requests. Any 2xx counts as
// healthy; the caller decides what other statuses mean.
func (c *Client) Health(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/api/health", nil, nil)
}

// ListSessions returns the sessions known to the server.
func (c *Client) ListSessions(ctx context.Context) ([]Session, error) {
	var sessions []Session
	if err := c.doJSON(ctx, http.MethodGet, "/api/session", nil, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

// CreateSession opens a session rooted at directory. The server rejects the
// call when directory is not accessible from the process.
func (c *Client) CreateSession(ctx context.Context, directory string) (Session, error) {
	req := createSessionRequest{Location: sessionLocation{Directory: directory}}
	var session Session
	if err := c.doJSON(ctx, http.MethodPost, "/api/session", req, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

// Prompt admits text into a session asynchronously and returns the receipt ID
// for that admission. A receipt only means the prompt was queued; the reply
// text must be read back with Messages once the session goes idle.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) (string, error) {
	path := sessionPath(sessionID) + "/prompt"
	var resp promptResponse
	if err := c.doJSON(ctx, http.MethodPost, path, promptRequest{Text: text}, &resp); err != nil {
		return "", err
	}
	return resp.Receipt, nil
}

// Messages returns the messages of a session in server order. The final
// assistant message is the authoritative source of the run's text.
func (c *Client) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	var messages []Message
	if err := c.doJSON(ctx, http.MethodGet, sessionPath(sessionID)+"/message", nil, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// ReplyPermission answers a pending permission request. decision is one of
// "once", "always" or "reject"; message is optional and omitted when empty.
func (c *Client) ReplyPermission(ctx context.Context, sessionID, permissionID, decision, message string) error {
	path := sessionPath(sessionID) + "/permission/" + url.PathEscape(permissionID) + "/reply"
	req := replyPermissionRequest{Decision: decision, Message: message}
	return c.doJSON(ctx, http.MethodPost, path, req, nil)
}

// SubscribeEvents opens the server event feed and returns a buffered channel of
// decoded events. The channel is closed when the stream ends or ctx is
// canceled; callers must keep receiving until then to avoid losing frames.
func (c *Client) SubscribeEvents(ctx context.Context) (<-chan Event, error) {
	const path = "/api/event"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build opencode %s %s request: %w", http.MethodGet, path, err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode %s %s: %w", http.MethodGet, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		return nil, responseError(http.MethodGet, path, resp)
	}

	events := make(chan Event, eventStreamBuffer)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		readEventStream(ctx, resp.Body, events)
	}()
	return events, nil
}

// doJSON performs one request/response call, encoding in as JSON when non-nil
// and decoding the body into out when non-nil. Neither the request body nor the
// prompt text is ever included in an error message.
func (c *Client) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode opencode %s %s request: %w", method, path, err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build opencode %s %s request: %w", method, path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("opencode %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return responseError(method, path, resp)
	}
	if out == nil {
		// Drain a bounded prefix so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode opencode %s %s response: %w", method, path, err)
	}
	return nil
}

// readEventStream decodes server-sent events from body until the stream ends
// or ctx is canceled. Malformed frames are skipped: the interface has no error
// channel, and a single bad frame must not tear down the feed. Unknown event
// types are preserved for the caller to count.
func readEventStream(ctx context.Context, body io.Reader, events chan<- Event) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxEventSize)

	var data []byte
	dispatch := func() bool {
		if len(data) == 0 {
			return true
		}
		var event Event
		err := json.Unmarshal(data, &event)
		data = data[:0]
		if err != nil {
			return true
		}
		select {
		case events <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if !dispatch() {
				return
			}
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		value := line[len("data:"):]
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		if len(data) > 0 {
			data = append(data, '\n')
		}
		data = append(data, value...)
	}
	dispatch()
}

// responseError formats a non-2xx reply. The bounded body is included because
// opencode puts the actionable reason there, but request bodies are never
// echoed so prompts and credentials cannot leak through this path.
func responseError(method, path string, resp *http.Response) error {
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	trimmed := strings.TrimSpace(string(detail))
	if trimmed == "" {
		return fmt.Errorf("opencode %s %s: status %d", method, path, resp.StatusCode)
	}
	return fmt.Errorf("opencode %s %s: status %d: %s", method, path, resp.StatusCode, trimmed)
}

// sessionPath builds the collection path for a session, escaping the ID so a
// hostile or malformed value cannot inject path segments.
func sessionPath(sessionID string) string {
	return "/api/session/" + url.PathEscape(sessionID)
}

// createSessionRequest mirrors the v2 body: the working directory travels
// inside a location object rather than as a top-level field.
type createSessionRequest struct {
	Location sessionLocation `json:"location"`
}

type sessionLocation struct {
	Directory string `json:"directory"`
}

type promptRequest struct {
	Text string `json:"text"`
}

type promptResponse struct {
	Receipt string `json:"receipt"`
}

type replyPermissionRequest struct {
	Decision string `json:"decision"`
	Message  string `json:"message,omitempty"`
}
