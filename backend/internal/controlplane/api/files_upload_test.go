package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
)

type uploadChunkParams struct {
	Directory string `json:"directory"`
	Path      string `json:"path"`
	Offset    int64  `json:"offset"`
	Data      string `json:"data"`
}

func uploadRecorder(t *testing.T, env *testEnv, target, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	env.mux.ServeHTTP(rec, req)
	return rec
}

func TestAPIFileUploadStreamsChunks(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	content := bytes.Repeat([]byte("a"), int(fileUploadChunkBytes)+10)
	offsets := make(chan int64, 2)
	go func() {
		for i := 0; i < 2; i++ {
			var frame protocol.Frame
			select {
			case frame = <-agent.frames:
			case <-time.After(5 * time.Second):
				t.Errorf("agent did not receive chunk %d", i)
				return
			}
			var p uploadChunkParams
			if err := json.Unmarshal(frame.Params, &p); err != nil {
				t.Errorf("decode params: %v", err)
				return
			}
			offsets <- p.Offset
			conn.Deliver(protocol.Frame{
				Type: protocol.FrameResponse, ID: frame.ID,
				Result: json.RawMessage(`{"path":"a.bin","size":1}`),
			})
		}
	}()

	target := "/api/nodes/" + rec.ID + "/files/upload?directory=" + url.QueryEscape("/work")
	httpRec := uploadRecorder(t, env, target, "a.bin", content)
	requireStatus(t, httpRec, http.StatusOK)
	if got := <-offsets; got != 0 {
		t.Fatalf("first offset = %d", got)
	}
	if got := <-offsets; got != fileUploadChunkBytes {
		t.Fatalf("second offset = %d", got)
	}
	if body := httpRec.Body.String(); !strings.Contains(body, `"path":"a.bin"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestAPIFileUploadSanitizesFilename(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	params := make(chan uploadChunkParams, 1)
	go func() {
		var frame protocol.Frame
		select {
		case frame = <-agent.frames:
		case <-time.After(5 * time.Second):
			t.Errorf("agent did not receive upload")
			return
		}
		var p uploadChunkParams
		_ = json.Unmarshal(frame.Params, &p)
		params <- p
		conn.Deliver(protocol.Frame{Type: protocol.FrameResponse, ID: frame.ID,
			Result: json.RawMessage(`{"path":"passwd","size":1}`)})
	}()

	target := "/api/nodes/" + rec.ID + "/files/upload?directory=" + url.QueryEscape("/work")
	httpRec := uploadRecorder(t, env, target, "../../etc/passwd", []byte("x"))
	requireStatus(t, httpRec, http.StatusOK)
	if got := <-params; got.Path != "passwd" {
		t.Fatalf("path = %q, want passwd", got.Path)
	}
}

func TestAPIFileUploadRejectsBadRequests(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, _, _ := env.addNode("dev-box")

	missingDir := uploadRecorder(t, env, "/api/nodes/"+rec.ID+"/files/upload", "a.txt", []byte("x"))
	requireErrorCode(t, missingDir, http.StatusBadRequest, "invalid_request")

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/"+rec.ID+"/files/upload?directory=%2Fwork", bytes.NewReader([]byte("x")))
	req.Header.Set("Content-Type", "application/json")
	httpRec := httptest.NewRecorder()
	env.mux.ServeHTTP(httpRec, req)
	requireErrorCode(t, httpRec, http.StatusBadRequest, "invalid_request")
}

func TestAPIFileUploadRejectsOversizeContentLength(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, _, _ := env.addNode("dev-box")

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/"+rec.ID+"/files/upload?directory=%2Fwork", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.ContentLength = maxFileUploadBytes + (2 << 20)
	httpRec := httptest.NewRecorder()
	env.mux.ServeHTTP(httpRec, req)
	requireErrorCode(t, httpRec, http.StatusRequestEntityTooLarge, "file_too_large")
}

func TestAPIFileUploadCreatesEmptyFile(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	params := make(chan uploadChunkParams, 1)
	go func() {
		var frame protocol.Frame
		select {
		case frame = <-agent.frames:
		case <-time.After(5 * time.Second):
			t.Errorf("agent did not receive upload")
			return
		}
		var p uploadChunkParams
		_ = json.Unmarshal(frame.Params, &p)
		params <- p
		conn.Deliver(protocol.Frame{Type: protocol.FrameResponse, ID: frame.ID,
			Result: json.RawMessage(`{"path":"empty.txt","size":0}`)})
	}()

	target := "/api/nodes/" + rec.ID + "/files/upload?directory=" + url.QueryEscape("/work")
	httpRec := uploadRecorder(t, env, target, "empty.txt", nil)
	requireStatus(t, httpRec, http.StatusOK)
	if got := <-params; got.Path != "empty.txt" || got.Data != "" || got.Offset != 0 {
		t.Fatalf("params = %+v", got)
	}
}
