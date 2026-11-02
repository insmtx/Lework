//go:build integration

package testutil

import (
	"testing"

	"github.com/insmtx/Lework/backend/config"
	"github.com/nats-io/nats.go"
)

func SetupNATS(t *testing.T, cfg *config.NATSConfig) *nats.Conn {
	t.Helper()

	nc, err := nats.Connect(cfg.URL)
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}

	t.Cleanup(func() { nc.Close() })

	return nc
}
