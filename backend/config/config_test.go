package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestConfigParsesWorkspaceRootAndLogLevel(t *testing.T) {
	var cfg Config
	body := []byte("workspace_root: /tmp/leros\nlog:\n  level: error\nserver:\n  port: \"8080\"\n")

	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	if cfg.WorkspaceRoot != "/tmp/leros" {
		t.Fatalf("workspace root = %q", cfg.WorkspaceRoot)
	}
	if cfg.Log.Level != "error" {
		t.Fatalf("log level = %q", cfg.Log.Level)
	}
}

func TestConfigParsesMCPConnectors(t *testing.T) {
	var cfg Config
	body := []byte(`mcp_connectors:
  - channel: netease-mail
    name: 邮箱
    status: active
    skill_code: connector-netease-mail
    bindings:
      skill_env:
        NETEASE_EMAIL_USER: email
    auth:
      type: form
      description: 输入邮箱地址和 IMAP/SMTP 授权码（非登录密码）。支持 163、126、yeah.net 等网易邮箱，以及其他支持 IMAP/SMTP 的邮箱。
      fields:
        - key: email
          label: 邮箱地址
          type: text
          required: true
  - channel: baidu-netdisk
    name: 百度网盘
    status: inactive
    transport: sse
    url: https://mcp-pan.baidu.com/sse
    bindings:
      mcp_headers:
        Authorization: "Bearer {{access_token}}"
    auth:
      type: oauth
      handler: baidu-netdisk
      oauth:
        app_key: ${BAIDU_NETDISK_APP_KEY}
        secret_key: ${BAIDU_NETDISK_SECRET_KEY}
        redirect_uri: https://leros.example.com/callback
        scopes: [basic, netdisk]
  - channel: catapi
    name: CatAPI
    status: active
    transport: http
    url: https://api.example.com/v6/api/mcp
    bindings:
      mcp_headers:
        Authorization: "Bearer {{api_key}}"
    auth:
      type: form
      description: 输入 CatAPI API Key，连接后即可使用 API 市场中的工具和服务。
      fields:
        - key: api_key
          label: API Key
          type: password
          required: true
`)

	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if len(cfg.MCPConnectors) != 3 {
		t.Fatalf("MCP connectors = %#v", cfg.MCPConnectors)
	}
	mail := cfg.MCPConnectors[0]
	if mail.SkillCode != "connector-netease-mail" || mail.Auth.Type != "form" ||
		mail.Auth.Description != "输入邮箱地址和 IMAP/SMTP 授权码（非登录密码）。支持 163、126、yeah.net 等网易邮箱，以及其他支持 IMAP/SMTP 的邮箱。" ||
		len(mail.Auth.Fields) != 1 || mail.Bindings.SkillEnv["NETEASE_EMAIL_USER"] != "email" {
		t.Fatalf("mail connector = %#v", mail)
	}
	baidu := cfg.MCPConnectors[1]
	if baidu.Status != "inactive" || baidu.Auth.OAuth == nil ||
		baidu.Auth.OAuth.Scopes[1] != "netdisk" ||
		baidu.Bindings.MCPHeaders["Authorization"] != "Bearer {{access_token}}" {
		t.Fatalf("baidu connector = %#v", baidu)
	}
	catapi := cfg.MCPConnectors[2]
	if catapi.Channel != "catapi" || catapi.Transport != "http" ||
		catapi.Bindings.MCPHeaders["Authorization"] != "Bearer {{api_key}}" ||
		catapi.Auth.Description != "输入 CatAPI API Key，连接后即可使用 API 市场中的工具和服务。" ||
		len(catapi.Auth.Fields) != 1 || catapi.Auth.Fields[0].Key != "api_key" {
		t.Fatalf("CatAPI connector = %#v", catapi)
	}
}

func TestDevelopmentServerExampleIncludesBearerBindings(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "deployments", "dev", "server.config.example.yaml"))
	if err != nil {
		t.Fatalf("read development server example: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal development server example: %v", err)
	}
	connectors := make(map[string]MCPConnectorConfig, len(cfg.MCPConnectors))
	for _, connector := range cfg.MCPConnectors {
		connectors[connector.Channel] = connector
	}
	catapi := connectors["catapi"]
	if catapi.URL != "https://api.example.com/v6/api/mcp" || catapi.Transport != "http" ||
		catapi.Bindings.MCPHeaders["Authorization"] != "Bearer {{api_key}}" || catapi.Auth.Description == "" {
		t.Fatalf("CatAPI example = %#v", catapi)
	}
	corekg := connectors["corekg"]
	if corekg.Auth.Type != "managed" || corekg.Auth.Handler != "corekg" ||
		corekg.Bindings.MCPHeaders["Authorization"] != "Bearer {{api_key}}" {
		t.Fatalf("CoreKG example = %#v", corekg)
	}
}
