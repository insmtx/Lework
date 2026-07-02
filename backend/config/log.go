package config

// LogConfig configures application logging.
type LogConfig struct {
	Level string `yaml:"level,omitempty" json:"level,omitempty"`
}
