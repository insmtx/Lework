package llmprotocol

import (
	"errors"
	"testing"
)

type testProtocolAdapter struct {
	protocol Protocol
}

func (a *testProtocolAdapter) Protocol() Protocol {
	return a.protocol
}

func (a *testProtocolAdapter) DecodeRequest(raw map[string]interface{}) (*IRRequest, error) {
	return nil, nil
}

func (a *testProtocolAdapter) EncodeRequest(ir *IRRequest) (map[string]interface{}, error) {
	return nil, nil
}

func (a *testProtocolAdapter) DecodeResponse(raw map[string]interface{}) (*IRResponse, error) {
	return nil, nil
}

func (a *testProtocolAdapter) EncodeResponse(ir *IRResponse) (map[string]interface{}, error) {
	return nil, nil
}

func (a *testProtocolAdapter) NewStreamState() interface{} {
	return nil
}

func (a *testProtocolAdapter) DecodeStreamEvent(raw map[string]interface{}, state interface{}) ([]*IRStreamEvent, error) {
	return nil, nil
}

func (a *testProtocolAdapter) EncodeStreamEvent(ir *IRStreamEvent, state interface{}) ([]map[string]interface{}, error) {
	return nil, nil
}

func withAdapterRegistry(t *testing.T, registry map[Protocol]ProtocolAdapter, err error) {
	t.Helper()

	previousAdapters := adapters
	previousRegistrationErr := registrationErr

	adapters = registry
	registrationErr = err

	t.Cleanup(func() {
		adapters = previousAdapters
		registrationErr = previousRegistrationErr
	})
}

func TestRegisterAdapterRegistersAndReturnsAdapter(t *testing.T) {
	withAdapterRegistry(t, map[Protocol]ProtocolAdapter{}, nil)

	adapter := &testProtocolAdapter{protocol: Protocol("test_protocol")}
	if err := RegisterAdapter(adapter); err != nil {
		t.Fatalf("RegisterAdapter returned error: %v", err)
	}

	got, err := GetAdapter(adapter.protocol)
	if err != nil {
		t.Fatalf("GetAdapter returned error: %v", err)
	}
	if got != adapter {
		t.Fatalf("GetAdapter returned %p, want %p", got, adapter)
	}
}

func TestRegisterAdapterAllowsSameInstanceDuplicate(t *testing.T) {
	adapter := &testProtocolAdapter{protocol: Protocol("test_protocol")}
	withAdapterRegistry(t, map[Protocol]ProtocolAdapter{adapter.protocol: adapter}, nil)

	if err := RegisterAdapter(adapter); err != nil {
		t.Fatalf("RegisterAdapter returned error for same instance duplicate: %v", err)
	}
}

func TestRegisterAdapterReturnsErrorForDifferentDuplicate(t *testing.T) {
	protocol := Protocol("test_protocol")
	withAdapterRegistry(t, map[Protocol]ProtocolAdapter{protocol: &testProtocolAdapter{protocol: protocol}}, nil)

	err := RegisterAdapter(&testProtocolAdapter{protocol: protocol})
	if err == nil {
		t.Fatal("RegisterAdapter returned nil error for duplicate protocol")
	}
}

func TestRegisterAdapterReturnsErrorForInvalidAdapter(t *testing.T) {
	withAdapterRegistry(t, map[Protocol]ProtocolAdapter{}, nil)

	var typedNil *testProtocolAdapter
	for name, adapter := range map[string]ProtocolAdapter{
		"nil":        nil,
		"typed_nil":  typedNil,
		"empty_name": &testProtocolAdapter{},
	} {
		if err := RegisterAdapter(adapter); err == nil {
			t.Fatalf("RegisterAdapter returned nil error for %s adapter", name)
		}
	}
}

func TestGetAdapterReturnsRegistrationError(t *testing.T) {
	want := errors.New("registration failed")
	withAdapterRegistry(t, map[Protocol]ProtocolAdapter{ProtocolOpenAIChat: &openAIChatAdapter{}}, want)

	_, err := GetAdapter(ProtocolOpenAIChat)
	if !errors.Is(err, want) {
		t.Fatalf("GetAdapter error = %v, want %v", err, want)
	}
}
