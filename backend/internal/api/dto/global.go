package dto

type GlobalConfigData struct {
	Device                *SparkDevice `json:"device,omitempty"`
	Edition               string       `json:"edition"`
	DeployMode            string       `json:"deploy_mode"`
	MaxOrgsPerUser        int          `json:"max_orgs_per_user"`
	PhoneCodeLoginEnabled bool         `json:"phone_code_login_enabled"`
}

// SparkDevice advertises device category without granting management permissions.
type SparkDevice struct {
	Type            string `json:"type"`
	ProtocolVersion int    `json:"protocol_version"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Management      bool   `json:"management"`
}
