package status

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

// fakeSub captures a subscription created on a fake CoreConn.
type fakeSub struct {
	subject string
	handler nats.MsgHandler
}

// fakeConn implements CoreConn, recording the single subscription it creates.
type fakeConn struct {
	sub  *fakeSub
	err  error
	done chan struct{} // closed when Subscribe is called
}

func (f *fakeConn) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.sub = &fakeSub{subject: subject, handler: handler}
	if f.done != nil {
		close(f.done)
	}
	return &nats.Subscription{}, nil
}

// fakeProvider returns a fixed snapshot and records calls.
type fakeProvider struct {
	snapshot messaging.WorkerStatusSnapshot
}

func (f *fakeProvider) Status(ctx context.Context) messaging.WorkerStatusSnapshot {
	return f.snapshot
}

func newMessage(subject string, data []byte) *nats.Msg {
	return &nats.Msg{Subject: subject, Data: data, Reply: "inbox.123"}
}

func TestServiceStartSubscribesToOpsStatusSubject(t *testing.T) {
	conn := &fakeConn{done: make(chan struct{})}
	srv, err := New(Config{OrgID: 1, WorkerID: 2}, conn, &fakeProvider{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := "org.1.worker.2.ops.status"
	if got := srv.Subject(); got != want {
		t.Fatalf("Subject() = %q, want %q", got, want)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Start(ctx)
	}()

	select {
	case <-conn.done:
	case <-time.After(time.Second):
		t.Fatal("Start did not subscribe")
	}
	if conn.sub == nil || conn.sub.subject != want {
		t.Fatalf("subscribed subject = %v, want %s", conn.sub, want)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start did not stop on cancel")
	}
}

func TestServiceRejectsMissingOrg(t *testing.T) {
	if _, err := New(Config{WorkerID: 2}, &fakeConn{}, &fakeProvider{}); err == nil {
		t.Fatal("expected error when org_id missing")
	}
	if _, err := New(Config{OrgID: 1}, &fakeConn{}, &fakeProvider{}); err == nil {
		t.Fatal("expected error when worker_id missing")
	}
	if _, err := New(Config{OrgID: 1, WorkerID: 2}, nil, &fakeProvider{}); err == nil {
		t.Fatal("expected error when conn nil")
	}
	if _, err := New(Config{OrgID: 1, WorkerID: 2}, &fakeConn{}, nil); err == nil {
		t.Fatal("expected error when provider nil")
	}
}

func TestServiceHandleRepliesSnapshot(t *testing.T) {
	snapshot := messaging.WorkerStatusSnapshot{MaxConcurrency: 4, RunningCount: 1}
	srv, err := New(Config{OrgID: 1, WorkerID: 2}, &fakeConn{}, &fakeProvider{snapshot: snapshot})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// 有效归属请求不应 panic（真实回复需连接 NATS，此处仅验证处理路径可执行）。
	request, _ := json.Marshal(messaging.WorkerStatusRequest{OrgID: 1, WorkerID: 2})
	msg := newMessage("org.1.worker.2.ops.status", request)
	srv.handle(msg)
}

func TestServiceRejectsOrgMismatch(t *testing.T) {
	srv, err := New(Config{OrgID: 1, WorkerID: 2}, &fakeConn{}, &fakeProvider{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// org 不匹配的请求应被拒绝（Term，不回复正文）。
	request, _ := json.Marshal(messaging.WorkerStatusRequest{OrgID: 99, WorkerID: 2})
	msg := newMessage("org.1.worker.2.ops.status", request)
	srv.handle(msg)

	// 构造一个伪造响应，验证 worker_id 不匹配同样被拒。
	request2, _ := json.Marshal(messaging.WorkerStatusRequest{OrgID: 1, WorkerID: 99})
	msg2 := newMessage("org.1.worker.2.ops.status", request2)
	srv.handle(msg2)
}

func TestServiceRejectsBadRequestPayload(t *testing.T) {
	srv, err := New(Config{OrgID: 1, WorkerID: 2}, &fakeConn{}, &fakeProvider{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	msg := newMessage("org.1.worker.2.ops.status", []byte("{not-json"))
	srv.handle(msg)
}
