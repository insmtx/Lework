//go:build integration

package mq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// TestSubscribeCoreResponds 验证 SubscribeCore 注册的字节 handler 可以被
// RequestBytes 触达，并把 handler 的返回值作为回复发回请求方。
func TestSubscribeCoreResponds(t *testing.T) {
	bus := setupNATS(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subErr := make(chan error, 1)
	go func() {
		subErr <- bus.SubscribeCore(ctx, "t.core.echo", "", func(_ context.Context, data []byte) []byte {
			return bytes.ToUpper(data)
		})
	}()

	// 订阅是异步生效的；在 5 秒内重试，直到 Core NATS 不再返回 no responders。
	reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer reqCancel()

	var reply []byte
	deadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		reply, err = bus.RequestBytes(reqCtx, "t.core.echo", []byte("ping"))
		if err == nil {
			break
		}
		if !errors.Is(err, ErrNoResponders) || time.Now().After(deadline) {
			t.Fatalf("RequestBytes: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if string(reply) != "PING" {
		t.Fatalf("reply = %q, want %q", reply, "PING")
	}

	cancel()
	select {
	case err := <-subErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SubscribeCore returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SubscribeCore did not return after context cancel")
	}
}

// TestPublishCoreDelivers 验证 PublishCore 通过 Core NATS 原样投递字节。
func TestPublishCoreDelivers(t *testing.T) {
	bus := setupNATS(t)

	sub, err := bus.Conn().SubscribeSync("t.core.raw")
	if err != nil {
		t.Fatalf("subscribe sync: %v", err)
	}
	defer sub.Unsubscribe()
	if err := bus.Conn().Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	payload := []byte{0x00, 0x01, 'b', 'y', 't', 'e', 0xff}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := bus.PublishCore(ctx, "t.core.raw", payload); err != nil {
		t.Fatalf("PublishCore: %v", err)
	}

	msg, err := sub.NextMsgWithContext(ctx)
	if err != nil {
		t.Fatalf("next msg: %v", err)
	}
	if !bytes.Equal(msg.Data, payload) {
		t.Fatalf("received %v, want %v", msg.Data, payload)
	}
}

// setupNATSJetStream 返回一个确认支持 JetStream 的 bus，供 pull/stream 用例使用。
//
// Core NATS 用例仍走 setupNATS（127.0.0.1:4222）。本机 devcj 集群
// （deployments/devcj）把带 --jetstream 的 NATS 暴露在 14224，因此当默认地址
// 未启用 JetStream 时回退到该地址；也可用 LEWORK_TEST_NATS_URL 显式指定。
// 所有候选地址都不可用或未启用 JetStream 时跳过，避免误报失败。
func setupNATSJetStream(t *testing.T) *natsBus {
	t.Helper()

	candidates := []string{"nats://127.0.0.1:4222", "nats://127.0.0.1:14224"}
	if custom := os.Getenv("LEWORK_TEST_NATS_URL"); custom != "" {
		candidates = []string{custom}
	}

	var lastErr error
	for _, url := range candidates {
		nc, err := nats.Connect(url)
		if err != nil {
			lastErr = fmt.Errorf("connect %s: %w", url, err)
			continue
		}
		js, err := nc.JetStream()
		if err != nil {
			nc.Close()
			lastErr = fmt.Errorf("jetstream %s: %w", url, err)
			continue
		}
		// 未启用 JetStream 的服务器对 JS API subject 没有订阅者。
		if _, err := js.StreamInfo("MQTEST_PROBE"); err != nil && !errors.Is(err, nats.ErrStreamNotFound) {
			nc.Close()
			lastErr = fmt.Errorf("jetstream probe %s: %w", url, err)
			continue
		}
		t.Logf("using JetStream-capable NATS at %s", url)
		t.Cleanup(func() { nc.Close() })
		return &natsBus{conn: nc, js: js, closed: atomic.Bool{}}
	}

	t.Skipf("no JetStream-capable NATS available: %v", lastErr)
	return nil
}

// TestSubscribePullDistributesOnce 验证两个使用同一 durable 的 pull 订阅者
// 会分摊消息，且每条消息只被处理一次。
func TestSubscribePullDistributesOnce(t *testing.T) {
	bus := setupNATSJetStream(t)

	nano := time.Now().UnixNano()
	stream := fmt.Sprintf("MQTEST_PULL_%d", nano)
	subject := fmt.Sprintf("mqtest.pull.%d", nano)
	durable := fmt.Sprintf("mqtest_pull_%d", nano)

	if _, err := bus.js.AddStream(&nats.StreamConfig{Name: stream, Subjects: []string{subject}}); err != nil {
		t.Fatalf("add stream: %v", err)
	}
	t.Cleanup(func() { _ = bus.js.DeleteStream(stream) })

	const total = 20
	for i := 0; i < total; i++ {
		if _, err := bus.js.Publish(subject, []byte(fmt.Sprintf("msg-%d", i))); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu      sync.Mutex
		handled = make(map[string]int)
	)
	handler := func(_ context.Context, data []byte) error {
		mu.Lock()
		handled[string(data)]++
		mu.Unlock()
		return nil
	}

	pullErr := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			pullErr <- bus.SubscribePull(ctx, stream, subject, durable, handler)
		}()
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-pullErr:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("SubscribePull failed early: %v", err)
			}
			t.Fatalf("SubscribePull returned early: %v", err)
		default:
		}

		mu.Lock()
		keys := len(handled)
		sum := 0
		duplicated := false
		for _, count := range handled {
			sum += count
			if count != 1 {
				duplicated = true
			}
		}
		snapshot := make(map[string]int, len(handled))
		for k, v := range handled {
			snapshot[k] = v
		}
		mu.Unlock()

		if keys == total && sum == total && !duplicated {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pull distribution incomplete: unique=%d sum=%d handled=%v", keys, sum, snapshot)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-pullErr:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("SubscribePull returned %v, want nil or context.Canceled", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("SubscribePull did not return after context cancel")
		}
	}
}

// TestPullConsumerDeliversOnlyNewMessages 验证 durable pull consumer 首次创建
// 时使用 DeliverNew：创建前留在 stream 里的历史消息不会被重新推送。
//
// 推送消费者依赖这个语义：stream 里可能保留很久以前的 permission.asked，
// 新实例启动时不能把历史审批全部再推一遍。
func TestPullConsumerDeliversOnlyNewMessages(t *testing.T) {
	bus := setupNATSJetStream(t)

	nano := time.Now().UnixNano()
	stream := fmt.Sprintf("MQTEST_NEW_%d", nano)
	subject := fmt.Sprintf("mqtest.new.%d", nano)
	durable := fmt.Sprintf("mqtest_new_%d", nano)

	if _, err := bus.js.AddStream(&nats.StreamConfig{Name: stream, Subjects: []string{subject}}); err != nil {
		t.Fatalf("add stream: %v", err)
	}
	t.Cleanup(func() { _ = bus.js.DeleteStream(stream) })

	// 先写入“历史”消息，再创建 consumer。
	for _, old := range []string{"old-1", "old-2"} {
		if _, err := bus.js.Publish(subject, []byte(old)); err != nil {
			t.Fatalf("publish %s: %v", old, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu       sync.Mutex
		received []string
	)
	pullErr := make(chan error, 1)
	go func() {
		pullErr <- bus.SubscribePull(ctx, stream, subject, durable, func(_ context.Context, data []byte) error {
			mu.Lock()
			received = append(received, string(data))
			mu.Unlock()
			return nil
		})
	}()

	// 等 durable 真正创建后再发布新消息，避免把“consumer 还没建好”当成投递失败。
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := bus.js.ConsumerInfo(stream, durable); err == nil {
			break
		} else if !errors.Is(err, nats.ErrConsumerNotFound) {
			t.Fatalf("inspect consumer: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("pull consumer was not created within 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := bus.js.Publish(subject, []byte("new-1")); err != nil {
		t.Fatalf("publish new message: %v", err)
	}

	deadline = time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := append([]string(nil), received...)
		mu.Unlock()
		if len(got) > 0 {
			if len(got) != 1 || got[0] != "new-1" {
				t.Fatalf("pull consumer received %v, want only new-1", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the new message")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 再留一段时间，确认历史消息不会被补投。
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	got := append([]string(nil), received...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "new-1" {
		t.Fatalf("pull consumer received %v after settling, want only new-1", got)
	}

	cancel()
	select {
	case err := <-pullErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("SubscribePull returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SubscribePull did not return after context cancel")
	}
}

// TestSubscribeBytesDelivers 验证 SubscribeBytes 以 DeliverNew 方式投递原始负载。
func TestSubscribeBytesDelivers(t *testing.T) {
	bus := setupNATSJetStream(t)

	nano := time.Now().UnixNano()
	stream := fmt.Sprintf("MQTEST_BYTES_%d", nano)
	subject := fmt.Sprintf("mqtest.bytes.%d", nano)

	if _, err := bus.js.AddStream(&nats.StreamConfig{Name: stream, Subjects: []string{subject}}); err != nil {
		t.Fatalf("add stream: %v", err)
	}
	t.Cleanup(func() { _ = bus.js.DeleteStream(stream) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan []byte, 4)
	subErr := make(chan error, 1)
	go func() {
		subErr <- bus.SubscribeBytes(ctx, subject, 0, func(_ context.Context, data []byte) error {
			received <- data
			return nil
		})
	}()

	// DeliverNew 只投递订阅之后的消息；短暂等待订阅注册完成再发布。
	if err := bus.Conn().Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	payload := []byte("state-event")
	if _, err := bus.js.Publish(subject, payload); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if !bytes.Equal(got, payload) {
			t.Fatalf("received %q, want %q", got, payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SubscribeBytes handler")
	}

	cancel()
	select {
	case err := <-subErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("SubscribeBytes returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SubscribeBytes did not return after context cancel")
	}
}

// TestRequestBytesNoResponders 验证没有订阅者时 RequestBytes 返回哨兵错误。
func TestRequestBytesNoResponders(t *testing.T) {
	bus := setupNATS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := bus.RequestBytes(ctx, "t.core.nobody", []byte("ping"))
	if !errors.Is(err, ErrNoResponders) {
		t.Fatalf("expected ErrNoResponders, got %v", err)
	}
}
