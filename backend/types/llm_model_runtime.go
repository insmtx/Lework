package types

// LLMModelReference excludes credentials when matching configured endpoints to local runtimes.
type LLMModelReference struct {
	ID      uint
	OrgID   uint
	Model   string
	BaseURL string
}

// LLMModelRuntime keeps an observed runtime state separate from administrative enablement.
type LLMModelRuntime struct {
	Source     string `json:"source"`
	State      string `json:"state"`
	ObservedAt int64  `json:"observed_at"`
	ValidUntil int64  `json:"valid_until"`
}
