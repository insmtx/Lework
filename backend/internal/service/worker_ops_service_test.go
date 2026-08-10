package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

// fakeRequester 实现 mq.CoreRequester，便于控制测试场景。
type fakeRequester struct {
	reply *nats.Msg
	err   error
}

func (f *fakeRequester) RequestReply(_ context.Context, _ string, _ any) (*nats.Msg, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.reply, nil
}

func TestQueryWorkerStatusSuccess(t *testing.T) {
	snapshot := messaging.WorkerStatusSnapshot{OrgID: 1, WorkerID: 2, SnapshotAt: 1700000000, MaxConcurrency: 4, RunningCount: 2}
	data, _ := json.Marshal(snapshot)
	svc := NewWorkerOpsService(&fakeRequester{reply: &nats.Msg{Data: data}}, time.Second)

	got, err := svc.QueryWorkerStatus(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("QueryWorkerStatus error = %v", err)
	}
	if got.MaxConcurrency != 4 || got.RunningCount != 2 {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestQueryWorkerStatusTimeout(t *testing.T) {
	svc := NewWorkerOpsService(&fakeRequester{err: context.DeadlineExceeded}, time.Second)
	_, err := svc.QueryWorkerStatus(context.Background(), 1, 2)
	if !errors.Is(err, ErrWorkerTimeout) {
		t.Fatalf("err = %v, want ErrWorkerTimeout", err)
	}
}

func TestQueryWorkerStatusNATSTimeout(t *testing.T) {
	svc := NewWorkerOpsService(&fakeRequester{err: nats.ErrTimeout}, time.Second)
	_, err := svc.QueryWorkerStatus(context.Background(), 1, 2)
	if !errors.Is(err, ErrWorkerTimeout) {
		t.Fatalf("err = %v, want ErrWorkerTimeout", err)
	}
}

func TestQueryWorkerStatusUnavailable(t *testing.T) {
	svc := NewWorkerOpsService(&fakeRequester{err: errors.New("nats connection broken")}, time.Second)
	_, err := svc.QueryWorkerStatus(context.Background(), 1, 2)
	if !errors.Is(err, ErrWorkerUnavailable) {
		t.Fatalf("err = %v, want ErrWorkerUnavailable", err)
	}
}

func TestQueryWorkerStatusNilRequester(t *testing.T) {
	svc := NewWorkerOpsService(nil, time.Second)
	_, err := svc.QueryWorkerStatus(context.Background(), 1, 2)
	if !errors.Is(err, ErrWorkerUnavailable) {
		t.Fatalf("err = %v, want ErrWorkerUnavailable", err)
	}
}

func TestQueryWorkerStatusBadResponse(t *testing.T) {
	svc := NewWorkerOpsService(&fakeRequester{reply: &nats.Msg{Data: []byte("{not-json")}}, time.Second)
	_, err := svc.QueryWorkerStatus(context.Background(), 1, 2)
	if !errors.Is(err, ErrWorkerBadResponse) {
		t.Fatalf("err = %v, want ErrWorkerBadResponse", err)
	}
}

func TestQueryWorkerStatusNoResponderUnavailable(t *testing.T) {
	svc := NewWorkerOpsService(&fakeRequester{err: nats.ErrNoResponders}, time.Second)
	_, err := svc.QueryWorkerStatus(context.Background(), 1, 2)
	if !errors.Is(err, ErrWorkerUnavailable) {
		t.Fatalf("err = %v, want ErrWorkerUnavailable", err)
	}
}

func TestQueryWorkerStatusRejectsMismatchedIdentity(t *testing.T) {
	data, _ := json.Marshal(messaging.WorkerStatusSnapshot{OrgID: 9, WorkerID: 2, SnapshotAt: 1700000000})
	svc := NewWorkerOpsService(&fakeRequester{reply: &nats.Msg{Data: data}}, time.Second)
	_, err := svc.QueryWorkerStatus(context.Background(), 1, 2)
	if !errors.Is(err, ErrWorkerBadResponse) {
		t.Fatalf("err = %v, want ErrWorkerBadResponse", err)
	}
}
