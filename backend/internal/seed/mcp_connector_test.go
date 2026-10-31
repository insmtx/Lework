package seed

import (
	"testing"

	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/types"
)

func TestConfiguredMCPConnectorSpecsMapsDefaultsAndAuthorization(t *testing.T) {
	specs := configuredMCPConnectorSpecs([]config.MCPConnectorConfig{
		{
			Channel: "example", Name: "Example",
			Bindings: config.MCPConnectorBindings{MCPHeaders: map[string]string{
				"Authorization": "Bearer {{token}}",
			}},
			Auth: config.MCPConnectorAuthConfig{
				Type: "form", Description: "Enter the token.",
				Fields: []config.MCPConnectorAuthField{{
					Key: "token", Label: "Token", Type: "password", Required: true,
				}},
			},
		},
	})
	if len(specs) != 1 {
		t.Fatalf("specs = %#v", specs)
	}
	spec := specs[0]
	if spec.Status != types.MCPChannelStatusActive || spec.AuthType != types.MCPChannelAuthTypeForm {
		t.Fatalf("defaults = %#v", spec)
	}
	if spec.AuthConfig.Description != "Enter the token." ||
		len(spec.AuthConfig.Fields) != 1 || spec.AuthConfig.Fields[0].Key != "token" ||
		spec.AuthConfig.Bindings.MCPHeaders["Authorization"] != "Bearer {{token}}" {
		t.Fatalf("authorization = %#v", spec.AuthConfig)
	}
}
