package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunServicesErrorCancelsPeers(t *testing.T) {
	cancelled := make(chan struct{})
	boom := errors.New("boom")
	services := []backendService{
		{name: "slow", run: func(ctx context.Context) error { <-ctx.Done(); close(cancelled); return nil }},
		{name: "failing", run: func(context.Context) error { return boom }},
	}
	err := runServices(context.Background(), services)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("peer was not cancelled")
	}
}

func TestRunServicesUnexpectedNilIsError(t *testing.T) {
	err := runServices(context.Background(), []backendService{
		{name: "quitter", run: func(context.Context) error { return nil }},
	})
	if err == nil || !strings.Contains(err.Error(), "quitter") {
		t.Fatalf("err = %v, want mention of quitter", err)
	}
}

func TestRunServicesContextCancelIsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	err := runServices(ctx, []backendService{
		{name: "a", run: func(c context.Context) error { <-c.Done(); return nil }},
		{name: "b", run: func(c context.Context) error { <-c.Done(); return nil }},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil on clean shutdown", err)
	}
}

func TestRunServicesClosesInReverseOrder(t *testing.T) {
	var order []string
	var mu sync.Mutex
	mk := func(name string) backendService {
		return backendService{name: name,
			run:   func(c context.Context) error { <-c.Done(); return nil },
			close: func() error { mu.Lock(); defer mu.Unlock(); order = append(order, name); return nil }}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	if err := runServices(ctx, []backendService{mk("serve"), mk("agent")}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "agent,serve" {
		t.Fatalf("close order = %v", order)
	}
}

// TestRunServicesJoinsCloseErrors 确认 close 错误被合并返回，且不掩盖主错误。
func TestRunServicesJoinsCloseErrors(t *testing.T) {
	boom := errors.New("boom")
	closeBoom := errors.New("close-boom")
	err := runServices(context.Background(), []backendService{
		{name: "failing", run: func(context.Context) error { return boom },
			close: func() error { return closeBoom }},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want primary boom", err)
	}
	if !errors.Is(err, closeBoom) {
		t.Fatalf("err = %v, want joined close-boom", err)
	}
}
