package seed

import (
	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/types"
)

// configuredMCPConnectorSpecs maps server configuration into database-neutral connector input.
func configuredMCPConnectorSpecs(configs []config.MCPConnectorConfig) []types.MCPConnectorSpec {
	specs := make([]types.MCPConnectorSpec, 0, len(configs))
	for _, item := range configs {
		auth := item.Auth
		spec := types.MCPConnectorSpec{
			Channel:     item.Channel,
			Name:        item.Name,
			Description: item.Description,
			Status:      item.Status,
			SkillCode:   item.SkillCode,
			Transport:   item.Transport,
			URL:         item.URL,
			Headers:     types.MCPChannelHeaders(copyStringMap(item.Headers)),
			AuthType:    auth.Type,
			AuthConfig: types.MCPChannelAuthConfig{
				Description: auth.Description,
				Fields:      makeMCPAuthFields(auth.Fields),
				Bindings:    makeMCPAuthBindings(item.Bindings),
				Handler:     auth.Handler,
				OAuth:       makeMCPOAuthConfig(auth.OAuth),
			},
		}
		if spec.Status == "" {
			spec.Status = types.MCPChannelStatusActive
		}
		if spec.AuthType == "" {
			spec.AuthType = types.MCPChannelAuthTypeNone
		}
		specs = append(specs, spec)
	}
	return specs
}

func makeMCPAuthFields(fields []config.MCPConnectorAuthField) []types.MCPChannelAuthField {
	result := make([]types.MCPChannelAuthField, 0, len(fields))
	for _, field := range fields {
		result = append(result, types.MCPChannelAuthField{
			Key: field.Key, Label: field.Label, Type: field.Type, Required: field.Required,
			Placeholder: field.Placeholder, Description: field.Description,
		})
	}
	return result
}

func makeMCPAuthBindings(bindings config.MCPConnectorBindings) types.MCPChannelAuthBindings {
	return types.MCPChannelAuthBindings{
		SkillEnv:   copyStringMap(bindings.SkillEnv),
		MCPHeaders: copyStringMap(bindings.MCPHeaders),
		MCPEnv:     copyStringMap(bindings.MCPEnv),
		MCPQuery:   copyStringMap(bindings.MCPQuery),
	}
}

func makeMCPOAuthConfig(value *config.MCPConnectorOAuthConfig) *types.MCPChannelOAuthConfig {
	if value == nil {
		return nil
	}
	return &types.MCPChannelOAuthConfig{
		AppKey: value.AppKey, SecretKey: value.SecretKey, RedirectURI: value.RedirectURI,
		Scopes: append([]string(nil), value.Scopes...),
	}
}

func copyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
