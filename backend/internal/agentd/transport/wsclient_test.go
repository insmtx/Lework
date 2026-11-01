package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/Lework/backend/internal/agentd/protocol"
)

func TestClientSendsHelloAndReadsWelcome(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var hello protocol.Frame
		if err := conn.ReadJSON(&hello); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		if hello.Type != protocol.FrameHello {
			t.Errorf("type = %q, want hello", hello.Type)
		}
		_ = conn.WriteJSON(protocol.Frame{Type: protocol.FrameWelcome, ID: hello.ID})
	}))
	defer srv.Close()

	ctx := context.Background()
	client, err := Dial(ctx, DialConfig{URL: "ws" + strings.TrimPrefix(srv.URL, "http")})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if err := client.Write(ctx, protocol.Frame{Type: protocol.FrameHello, ID: "1"}); err != nil {
		t.Fatal(err)
	}
	frame, err := client.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != protocol.FrameWelcome {
		t.Fatalf("type = %q, want welcome", frame.Type)
	}
}

// idleServer accepts a WebSocket connection and then keeps it open without
// sending anything, so the client side blocks in Read.
func idleServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestDialRejectsUnsupportedScheme(t *testing.T) {
	if _, err := Dial(context.Background(), DialConfig{URL: "http://127.0.0.1:1"}); err == nil {
		t.Fatal("Dial accepted an http URL, want error")
	}
}

func TestReadHonoursContextDeadline(t *testing.T) {
	client, err := Dial(context.Background(), DialConfig{URL: wsURL(idleServer(t))})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := client.Read(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestReadUnblocksOnCancelWithoutDeadline(t *testing.T) {
	client, err := Dial(context.Background(), DialConfig{URL: wsURL(idleServer(t))})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	if _, err := client.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestWriteRejectsCanceledContext(t *testing.T) {
	client, err := Dial(context.Background(), DialConfig{URL: wsURL(idleServer(t))})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := client.Write(ctx, protocol.Frame{Type: protocol.FrameHeartbeat}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	client, err := Dial(context.Background(), DialConfig{URL: wsURL(idleServer(t))})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
}

func TestDialPinsTLSSHA256Fingerprint(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the mismatch case logs a handshake error by design
	srv.StartTLS()
	defer srv.Close()

	sum := sha256.Sum256(srv.Certificate().Raw)
	fingerprint := hex.EncodeToString(sum[:])
	endpoint := "wss" + strings.TrimPrefix(srv.URL, "https")

	client, err := Dial(context.Background(), DialConfig{URL: endpoint, TLSFingerprint: fingerprint})
	if err != nil {
		t.Fatalf("Dial with matching fingerprint: %v", err)
	}
	defer client.Close()

	wrong := strings.Repeat("0", sha256.Size*2)
	if _, err := Dial(context.Background(), DialConfig{URL: endpoint, TLSFingerprint: wrong}); err == nil {
		t.Fatal("Dial accepted a mismatched fingerprint, want error")
	}
}

func TestDialRejectsMalformedFingerprint(t *testing.T) {
	endpoint := "wss://127.0.0.1:1"
	for _, fingerprint := range []string{"sha256:zz", strings.Repeat("a", 63)} {
		if _, err := Dial(context.Background(), DialConfig{URL: endpoint, TLSFingerprint: fingerprint}); err == nil {
			t.Fatalf("Dial accepted malformed fingerprint %q, want error", fingerprint)
		}
	}
}
