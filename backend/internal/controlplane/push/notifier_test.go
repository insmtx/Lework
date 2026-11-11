package push

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

type fakeDevices struct {
	devices []store.Device
	err     error
}

func (f fakeDevices) ListDevices(context.Context) ([]store.Device, error) { return f.devices, f.err }

type fakeSender struct {
	batches [][]Message
	err     error
}

func (f *fakeSender) Send(_ context.Context, msgs []Message) error {
	f.batches = append(f.batches, msgs)
	return f.err
}

func permissionAsked(node, session, permission string) event.Event {
	data, _ := json.Marshal(map[string]string{
		"id":      permission,
		"action":  "bash",
		"message": "run tests",
	})
	return event.Event{NodeID: node, SessionID: session, Type: "permission.asked", Data: data, Time: time.Now()}
}

func TestNotifierSendsOneMessagePerDevice(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{
		{ID: "d1", PushToken: "tok-a", Platform: "android"},
		{ID: "d2", PushToken: "tok-b", Platform: "android"},
	}}, sender, func(string) string { return "dev-box" })

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))

	if len(sender.batches) != 1 || len(sender.batches[0]) != 2 {
		t.Fatalf("batches = %+v, want one batch of two", sender.batches)
	}
	msg := sender.batches[0][0]
	if msg.To != "tok-a" || msg.Tag != "ses_1" || msg.ChannelID != "permissions" || msg.Priority != "high" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.Data["node"] != "node_1" || msg.Data["session"] != "ses_1" || msg.Data["permission"] != "perm_1" {
		t.Fatalf("data = %+v", msg.Data)
	}
	if !strings.Contains(msg.Title, "dev-box") {
		t.Fatalf("title = %q, want node label", msg.Title)
	}
	if !strings.Contains(msg.Body, "bash") {
		t.Fatalf("body = %q, want the permission action", msg.Body)
	}
}

func TestNotifierIgnoresOtherEvents(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "message.part.updated",
	})

	if len(sender.batches) != 0 {
		t.Fatalf("sent %d batches, want 0", len(sender.batches))
	}
}

func TestNotifierDeduplicatesSamePermission(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	ev := permissionAsked("node_1", "ses_1", "perm_1")
	n.Handle(context.Background(), ev)
	n.Handle(context.Background(), ev)

	if len(sender.batches) != 1 {
		t.Fatalf("sent %d batches, want 1", len(sender.batches))
	}
}

func TestNotifierSurvivesSendAndLookupFailure(t *testing.T) {
	sender := &fakeSender{err: errors.New("expo down")}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))
	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_2"))

	if len(sender.batches) != 2 {
		t.Fatalf("sent %d batches, want 2 (failures must not stop later events)", len(sender.batches))
	}

	broken := NewNotifier(fakeDevices{err: errors.New("db down")}, sender, func(string) string { return "n" })
	broken.Handle(context.Background(), permissionAsked("node_1", "ses_2", "perm_3"))
}

func TestNotifierSkipsPermissionWithoutID(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: json.RawMessage(`{"action":"bash"}`),
	})

	if len(sender.batches) != 0 {
		t.Fatalf("sent %d batches, want 0", len(sender.batches))
	}
}

type blockingSender struct {
	release chan struct{}
}

func (b *blockingSender) Send(context.Context, []Message) error {
	<-b.release
	return nil
}

func TestRunExitsOnCancelWhileSending(t *testing.T) {
	sender := &blockingSender{release: make(chan struct{})}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	ctx, cancel := context.WithCancel(context.Background())
	hub := event.NewHub()
	done := make(chan struct{})
	go func() {
		n.Run(ctx, hub)
		close(done)
	}()

	for i := 0; i < 20; i++ {
		hub.Publish(permissionAsked("node_1", "ses_1", strconv.Itoa(i)))
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after cancellation while a send was blocked")
	}
	close(sender.release)
}
