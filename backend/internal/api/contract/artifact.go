package contract

import (
	"context"
	"io"
	"time"
)

// ArtifactService defines task artifact query and download behavior.
type ArtifactService interface {
	ListTaskArtifacts(ctx context.Context, taskPublicID string) ([]Artifact, error)
	GetArtifact(ctx context.Context, artifactPublicID string) (*ArtifactDetail, error)
	GetArtifactDownload(ctx context.Context, artifactPublicID string) (*ArtifactDownload, error)
}

// Artifact is the public response shape for a generated file.
type Artifact struct {
	ArtifactID   string    `json:"artifact_id"`
	Title        string    `json:"title"`
	Filename     string    `json:"filename,omitempty"`
	Description  string    `json:"description,omitempty"`
	ArtifactType string    `json:"artifact_type"`
	MimeType     string    `json:"mime_type,omitempty"`
	FileSize     int64     `json:"file_size,omitempty"`
	Sha256       string    `json:"sha256,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
}

// ArtifactDetail is the full detail response for a single artifact.
type ArtifactDetail struct {
	Artifact
	RelativePath string `json:"relative_path,omitempty"`
	FilePublicID string `json:"file_public_id,omitempty"`
	Source       string `json:"source,omitempty"`
	ExportFormat string `json:"export_format,omitempty"`
	Version      int    `json:"version,omitempty"`
	Status       string `json:"status,omitempty"`
}

// ArtifactDownload contains a file stream and HTTP response metadata.
type ArtifactDownload struct {
	FileName string
	MimeType string
	Size     int64
	Reader   io.ReadCloser
}
