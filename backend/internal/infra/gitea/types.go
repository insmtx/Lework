package gitea

import "time"

type CreateRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private"`
	AutoInit    bool   `json:"auto_init"`
}

type RepoInfo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
}

type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

type FileContent struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Size     int64  `json:"size"`
}

type CreateFileRequest struct {
	Content string `json:"content"`
	Message string `json:"message"`
	Branch  string `json:"branch,omitempty"`
}

type TokenResponse struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Token          string    `json:"token"`
	TokenLastEight string    `json:"token_last_eight"`
	Created        time.Time `json:"created_at"`
}

type GenerateTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type GeneratedToken struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}
