package opencode

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCreatePersistentTerminal(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/experimental/session/ses_1/terminal" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"pty_1","title":"term","command":"/bin/sh","cwd":"/work","status":"running","size":{"cols":80,"rows":24}}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).CreatePersistentTerminal(context.Background(), "ses_1", CreateTerminalRequest{
		Command: "/bin/sh",
		Cwd:     "/work",
		Title:   "term",
		Size:    TerminalSize{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "pty_1" || got.Cwd != "/work" {
		t.Fatalf("terminal = %#v", got)
	}
	if args, ok := body["args"].([]any); !ok || len(args) != 0 {
		t.Fatalf("args = %#v", body["args"])
	}
	if env, ok := body["env"].(map[string]any); !ok || len(env) != 0 {
		t.Fatalf("env = %#v", body["env"])
	}
}

func TestConnectPersistentTerminalRequestsTicket(t *testing.T) {
	connected := make(chan *url.URL, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/experimental/persistent-pty/pty_1/connect-token", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-opencode-ticket"); got != "1" {
			t.Errorf("ticket marker = %q", got)
		}
		if got := r.Header.Get("Origin"); !strings.HasPrefix(got, "http://127.0.0.1:") && !strings.HasPrefix(got, "http://localhost:") {
			t.Errorf("origin = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":{"ticket":"t1","expires_in":60}}`))
	})
	mux.HandleFunc("GET /api/experimental/persistent-pty/pty_1/connect", func(w http.ResponseWriter, r *http.Request) {
		connected <- r.URL
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn, err := New(srv.URL, srv.Client()).ConnectPersistentTerminal(context.Background(), "pty_1")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	select {
	case got := <-connected:
		q := got.Query()
		if q.Get("ticket") != "t1" || q.Get("cursor") != "0" || q.Get("role") != "controller" {
			t.Fatalf("query = %v", q)
		}
		if q.Get("input_protocol") != "1" || q.Get("takeover") != "true" || q.Get("attachment_id") == "" {
			t.Fatalf("query = %v", q)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("websocket was not connected")
	}
}

func TestTerminalInputAndResizeFrames(t *testing.T) {
	frames := make(chan []byte, 4)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/experimental/persistent-pty/pty_1/connect-token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"ticket":"t1","expires_in":60}}`))
	})
	mux.HandleFunc("GET /api/experimental/persistent-pty/pty_1/connect", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.BinaryMessage {
				frames <- data
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn, err := New(srv.URL, srv.Client()).ConnectPersistentTerminal(context.Background(), "pty_1")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteInput(context.Background(), []byte("echo hi\r")); err != nil {
		t.Fatal(err)
	}
	input := receiveFrame(t, frames)
	if len(input) != 5+len("echo hi\r") || input[0] != 1 {
		t.Fatalf("input frame = %x", input)
	}
	if cols, rows := binary.BigEndian.Uint16(input[1:3]), binary.BigEndian.Uint16(input[3:5]); cols != 80 || rows != 24 {
		t.Fatalf("input size = %dx%d", cols, rows)
	}
	if string(input[5:]) != "echo hi\r" {
		t.Fatalf("input payload = %q", input[5:])
	}

	if err := conn.Resize(context.Background(), TerminalSize{Cols: 100, Rows: 40}); err != nil {
		t.Fatal(err)
	}
	resize := receiveFrame(t, frames)
	if len(resize) != 5 || resize[0] != 0 {
		t.Fatalf("resize frame = %x", resize)
	}
	if cols, rows := binary.BigEndian.Uint16(resize[1:3]), binary.BigEndian.Uint16(resize[3:5]); cols != 100 || rows != 40 {
		t.Fatalf("resize size = %dx%d", cols, rows)
	}
}

func receiveFrame(t *testing.T, frames <-chan []byte) []byte {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal frame")
		return nil
	}
}

func TestTerminalReadSkipsControlText(t *testing.T) {
	written := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/experimental/persistent-pty/pty_1/connect-token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"ticket":"t1","expires_in":60}}`))
	})
	mux.HandleFunc("GET /api/experimental/persistent-pty/pty_1/connect", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"attached","role":"controller"}`))
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte("hello"))
		close(written)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn, err := New(srv.URL, srv.Client()).ConnectPersistentTerminal(context.Background(), "pty_1")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	<-written

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kind, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.BinaryMessage || string(data) != "hello" {
		t.Fatalf("read = kind %d data %q", kind, data)
	}
}

func TestResizeAndClosePersistentTerminal(t *testing.T) {
	var resizeBody map[string]any
	var resizePath, closePath string
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/experimental/persistent-pty/pty_1", func(w http.ResponseWriter, r *http.Request) {
		resizePath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&resizeBody); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	})
	mux.HandleFunc("DELETE /api/experimental/persistent-pty/pty_1", func(w http.ResponseWriter, r *http.Request) {
		closePath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	if err := client.ResizePersistentTerminal(context.Background(), "pty_1", TerminalSize{Cols: 100, Rows: 40}); err != nil {
		t.Fatal(err)
	}
	if err := client.ClosePersistentTerminal(context.Background(), "pty_1"); err != nil {
		t.Fatal(err)
	}
	if resizePath != "/api/experimental/persistent-pty/pty_1" || closePath != resizePath {
		t.Fatalf("paths = %q %q", resizePath, closePath)
	}
	size, _ := resizeBody["size"].(map[string]any)
	if size["cols"] != float64(100) || size["rows"] != float64(40) {
		t.Fatalf("resize body = %#v", resizeBody)
	}
}
