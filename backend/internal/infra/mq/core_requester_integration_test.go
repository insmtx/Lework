//go:build integration

package mq

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

// setupNATS connects a fresh natsBus against the shared integration NATS.
// It connects directly rather than via testutil, because testutil depends on
// service which depends on mq (import cycle for this package's tests).
// Requires a NATS server at nats://127.0.0.1:4222.
func setupNATS(t *testing.T) *natsBus {
	t.Helper()
	nc, err := nats.Connect("nats://127.0.0.1:4222")
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(func() { nc.Close() })
	bus := &natsBus{conn: nc, closed: atomic.Bool{}}
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	bus.js = js
	return bus
}

func TestRequestReplySuccess(t *testing.T) {
	bus := setupNATS(t)
	subject, _ := messaging.WorkerOpsStatusSubject(1, 2)

	replyPayload, _ := json.Marshal(messaging.WorkerStatusSnapshot{MaxConcurrency: 4, RunningCount: 1})

	// Worker 侧：订阅 ops.status，回复运行快照。
	_, err := bus.Conn().Subscribe(subject, func(msg *nats.Msg) {
		var req messaging.WorkerStatusRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			_ = msg.Respond([]byte(`{"error":"bad request"}`))
			return
		}
		if req.OrgID != 1 || req.WorkerID != 2 {
			_ = msg.Respond([]byte(`{"error":"org mismatch"}`))
			return
		}
		_ = msg.Respond(replyPayload)
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply, err := bus.RequestReply(ctx, subject, messaging.WorkerStatusRequest{OrgID: 1, WorkerID: 2})
	if err != nil {
		t.Fatalf("RequestReply: %v", err)
	}

	var snapshot messaging.WorkerStatusSnapshot
	if err := json.Unmarshal(reply.Data, &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snapshot.MaxConcurrency != 4 || snapshot.RunningCount != 1 {
		t.Fatalf("snapshot = %+v, want max_concurrency=4 running=1", snapshot)
	}
}

func TestRequestReplyTimeout(t *testing.T) {
	bus := setupNATS(t)
	subject, _ := messaging.WorkerOpsStatusSubject(3, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := bus.RequestReply(ctx, subject, messaging.WorkerStatusRequest{OrgID: 3, WorkerID: 4})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func TestRequestReplyNoResponderNoReply(t *testing.T) {
	bus := setupNATS(t)
	subject, _ := messaging.WorkerOpsStatusSubject(5, 6)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := bus.RequestReply(ctx, subject, messaging.WorkerStatusRequest{OrgID: 5, WorkerID: 6})
	if !errors.Is(err, nats.ErrNoResponders) {
		t.Fatalf("expected ErrNoResponders, got %v", err)
	}
}
