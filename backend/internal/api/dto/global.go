package dto

type GlobalConfigData struct {
	Edition               string `json:"edition"`
	DeployMode            string `json:"deploy_mode"`
	MaxOrgsPerUser        int    `json:"max_orgs_per_user"`
	PhoneCodeLoginEnabled bool   `json:"phone_code_login_enabled"`
}
