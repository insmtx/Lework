package config

import (
	"testing"

	"gopkg.in/yaml.v2"
)

func TestLogsConfigYAMLAndConversion(t *testing.T) {
	input := []byte(`
logger:
  default:
    - writer: console
      level: debug
    - writer: file
      level: info
      filename: /tmp/lework.log
      maxsize: 10
      maxage: 15
      maxbackups: 5
      localtime: true
      compress: true
    - writer: cls
      level: info
      tencent_cls:
        topic_id: topic-id
        secret_id: secret-id
        secret_key: secret-key
        region: ap-beijing
        endpoint: ap-beijing.cls.tencentcs.com
`)

	var cfg Config
	if err := yaml.Unmarshal(input, &cfg); err != nil {
		t.Fatalf("unmarshal logger config: %v", err)
	}
	converted, err := cfg.Logger.ToYGConfig()
	if err != nil {
		t.Fatalf("convert logger config: %v", err)
	}
	outputs := converted["default"]
	if len(outputs) != 3 {
		t.Fatalf("default outputs = %d, want 3", len(outputs))
	}
	if outputs[1].Logger == nil || outputs[1].Logger.Filename != "/tmp/lework.log" {
		t.Fatalf("file logger was not converted correctly")
	}
	if outputs[2].TencentCLS == nil || outputs[2].TencentCLS.TopicID != "topic-id" {
		t.Fatalf("CLS logger was not converted correctly")
	}
}

func TestLogsConfigRejectsIncompleteCLS(t *testing.T) {
	cfg := LogsConfig{
		"default": {{Writer: "cls"}},
	}
	if _, err := cfg.ToYGConfig(); err == nil {
		t.Fatal("expected incomplete CLS configuration error")
	}
}
