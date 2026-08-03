package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

const skillFileName = "SKILL.md"
const skillMetadataFileName = ".skill-metadata"

type storedSkillMetadata struct {
	Source  string `json:"source"`
	SkillID string `json:"skill_id"`
}

// ErrSkillNotFound is returned when a skill cannot be found in the catalog.
type ErrSkillNotFound struct {
	Name    string
	Message string
}

func (e *ErrSkillNotFound) Error() string {
	return e.Message
}

// ErrSkillManifestMismatch is returned when a skill directory is found but
// its manifest name does not match the requested name.
type ErrSkillManifestMismatch struct {
	RequestedName string
	ManifestName  string
	Path          string
}

// Catalog reads Skill documents from one explicit root directory.
// A Catalog is immutable and safe to share across concurrent runs.
type Catalog struct {
	root string
}

// NewCatalog creates a catalog rooted at rootDir. The directory is created on
// first scan so an empty per-run skill view is a valid catalog.
func NewCatalog(rootDir string) (*Catalog, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("skill root is required")
	}
	absolute, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve skill root: %w", err)
	}
	return &Catalog{root: absolute}, nil
}

// Root returns the absolute directory scanned by this catalog.
func (c *Catalog) Root() string {
	if c == nil {
		return ""
	}
	return c.root
}

func defaultCatalog() (*Catalog, error) {
	dir, err := leros.SkillsDir()
	if err != nil {
		return nil, fmt.Errorf("resolve skill directory: %w", err)
	}
	return NewCatalog(dir)
}

// NewCatalogFromDefault creates a catalog for the worker-wide legacy skill root.
// New run execution should use NewCatalog with its explicitly prepared SkillDir.
func NewCatalogFromDefault() (*Catalog, error) {
	return defaultCatalog()
}

func (e *ErrSkillManifestMismatch) Error() string {
	if e.ManifestName != "" {
		return fmt.Sprintf("Skill %q found at path %s but its manifest name is %q.", e.RequestedName, filepath.ToSlash(e.Path), e.ManifestName)
	}
	return fmt.Sprintf("Skill %q found at path %s but its manifest name does not match.", e.RequestedName, filepath.ToSlash(e.Path))
}

// List returns skill summaries found in the default Leros skills directory,
// sorted by name. The skills directory is created if it does not exist.
func List() ([]Summary, error) {
	catalog, err := defaultCatalog()
	if err != nil {
		return nil, err
	}
	return catalog.List()
}

// List returns all Skill summaries under this catalog root.
func (c *Catalog) List() ([]Summary, error) {
	entries, _, err := c.readAllSkills()
	if err != nil {
		return nil, err
	}

	summaries := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		summaries = append(summaries, entry.Summary())
	}

	slices.SortFunc(summaries, func(left, right Summary) int {
		return strings.Compare(left.Name, right.Name)
	})

	return summaries, nil
}

// Get returns a full skill entry by name from the default Leros skills directory.
// It first tries the direct path <skillsDir>/<name>/SKILL.md (case-insensitive).
// If not found, it recursively walks the skills directory looking for SKILL.md files
// whose containing directory name matches name (case-insensitive).
// When a matching directory is found but the manifest name differs, the error
// describes the mismatch.
func Get(name string) (*Entry, error) {
	catalog, err := defaultCatalog()
	if err != nil {
		return nil, err
	}
	return catalog.Get(name)
}

// Get returns a full Skill entry by name from this catalog root.
func (c *Catalog) Get(name string) (*Entry, error) {
	dir := c.Root()
	if dir == "" {
		return nil, fmt.Errorf("skill catalog is not initialized")
	}

	// Phase 1: try direct path <skillsDir>/<name>/SKILL.md.
	directDir := filepath.Join(dir, name)
	directPath := filepath.Join(directDir, skillFileName)
	if entry, mismatch := tryParseSkillFile(directPath, name); entry != nil {
		return entry, nil
	} else if mismatch != "" {
		// Extract manifest name for typed error.
		manifestName := ""
		if raw, readErr := os.ReadFile(directPath); readErr == nil {
			if m, _, _ := ParseDocument(raw); m != nil {
				manifestName = m.Name
			}
		}
		return nil, &ErrSkillManifestMismatch{
			RequestedName: name,
			ManifestName:  manifestName,
			Path:          directPath,
		}
	}

	// Phase 2: walk all skill directories for a case-insensitive name match.
	var found *Entry
	var foundMismatch *ErrSkillManifestMismatch
	_ = WalkSkillDirs(dir, func(subDir, skillMDPath string) error {
		if !strings.EqualFold(subDir, name) {
			return nil
		}
		// Skip the direct path already tried in Phase 1.
		if subDir == name {
			return nil
		}

		entry, mismatch := tryParseSkillFile(skillMDPath, name)
		if entry != nil {
			found = entry
			return filepath.SkipAll
		}
		if mismatch != "" {
			manifestName := ""
			if raw, readErr := os.ReadFile(skillMDPath); readErr == nil {
				if m, _, _ := ParseDocument(raw); m != nil {
					manifestName = m.Name
				}
			}
			foundMismatch = &ErrSkillManifestMismatch{
				RequestedName: name,
				ManifestName:  manifestName,
				Path:          skillMDPath,
			}
			return filepath.SkipAll
		}
		return nil
	})

	if found != nil {
		return found, nil
	}
	if foundMismatch != nil {
		return nil, foundMismatch
	}

	return nil, &ErrSkillNotFound{Name: name, Message: fmt.Sprintf("skill %q not found", name)}
}

// tryParseSkillFile reads and parses a SKILL.md file, validates the manifest name
// matches the requested name (case-insensitive), and returns the Entry.
// Returns (nil, "") if the file doesn't exist or fails to parse.
// Returns (nil, mismatchMsg) if parsed successfully but the manifest name doesn't match.
// Returns (entry, "") on success.
func tryParseSkillFile(path, requestedName string) (*Entry, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}

	manifest, body, parseErr := ParseDocument(raw)
	if parseErr != nil {
		return nil, ""
	}

	dirName := filepath.Base(filepath.Dir(path))
	manifest.Normalize(dirName)

	if !strings.EqualFold(manifest.Name, requestedName) {
		mismatch := fmt.Sprintf("Skill %q found at path %s but its manifest name is %q.", requestedName, filepath.ToSlash(path), manifest.Name)
		return nil, mismatch
	}

	absDir := filepath.Dir(path)
	entry := &Entry{
		Manifest:    *manifest,
		Body:        body,
		Dir:         filepath.ToSlash(dirName),
		Path:        skillFileName,
		AbsoluteDir: filepath.ToSlash(absDir),
	}
	applyStoredMetadata(entry)
	return entry, ""
}

// ReadFile reads a supporting file from a skill directory in the default Leros
// skills directory. It validates the path to prevent directory traversal.
func ReadFile(name string, relativePath string) ([]byte, error) {
	catalog, err := defaultCatalog()
	if err != nil {
		return nil, err
	}
	return catalog.ReadFile(name, relativePath)
}

// ReadFile reads a supporting file from this catalog.
func (c *Catalog) ReadFile(name string, relativePath string) ([]byte, error) {
	entry, err := c.Get(name)
	if err != nil {
		return nil, err
	}

	root := filepath.Join(c.Root(), filepath.FromSlash(entry.Dir))
	targetPath, err := resolveInside(root, relativePath)
	if err != nil {
		return nil, fmt.Errorf("invalid skill file path %q: %w", relativePath, err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("read skill file %s: %w", targetPath, err)
	}

	return content, nil
}

// ListFiles returns supporting files in a skill directory under the default Leros
// skills directory, excluding SKILL.md. Results are sorted. If limit > 0, at most
// limit files are returned.
func ListFiles(name string, limit int) ([]string, error) {
	catalog, err := defaultCatalog()
	if err != nil {
		return nil, err
	}
	return catalog.ListFiles(name, limit)
}

// ListFiles returns supporting files in one Skill directory in this catalog.
func (c *Catalog) ListFiles(name string, limit int) ([]string, error) {
	entry, err := c.Get(name)
	if err != nil {
		return nil, err
	}

	root := filepath.Join(c.Root(), filepath.FromSlash(entry.Dir))
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve skill directory %s: %w", name, err)
	}
	files := make([]string, 0)
	err = filepath.WalkDir(resolvedRoot, func(filePath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(filePath) == skillFileName {
			return nil
		}

		rel, relErr := filepath.Rel(resolvedRoot, filePath)
		if relErr != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		if limit > 0 && len(files) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list skill files %s: %w", name, err)
	}

	slices.Sort(files)
	return files, nil
}

// readAllSkills scans the default Leros skills directory and returns all parsed
// entries keyed by manifest name. It only scans direct subdirectories (non-recursive).
// The skills directory is created if it does not exist.
func (c *Catalog) readAllSkills() (map[string]*Entry, string, error) {
	dir := c.Root()
	if dir == "" {
		return nil, "", fmt.Errorf("skill catalog is not initialized")
	}

	if err := os.MkdirAll(filepath.FromSlash(dir), 0o755); err != nil {
		return nil, "", fmt.Errorf("create skill directory %s: %w", dir, err)
	}

	entries := make(map[string]*Entry)
	err := WalkSkillDirs(dir, func(subDir, skillPath string) error {
		raw, readErr := os.ReadFile(skillPath)
		if readErr != nil {
			return nil
		}

		manifest, body, parseErr := ParseDocument(raw)
		if parseErr != nil {
			return fmt.Errorf("parse skill file %s: %w", skillPath, parseErr)
		}

		manifest.Normalize(subDir)

		entry := &Entry{
			Manifest:    *manifest,
			Body:        body,
			Dir:         subDir,
			Path:        filepath.ToSlash(filepath.Join(subDir, skillFileName)),
			AbsoluteDir: filepath.ToSlash(filepath.Join(dir, subDir)),
		}
		applyStoredMetadata(entry)
		if _, exists := entries[entry.Manifest.Name]; exists {
			return fmt.Errorf("duplicate skill name %q", entry.Manifest.Name)
		}
		entries[entry.Manifest.Name] = entry
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	return entries, dir, nil
}

func applyStoredMetadata(entry *Entry) {
	if entry == nil {
		return
	}
	metadata, err := readStoredMetadata(entry.AbsoluteDir)
	if err != nil {
		return
	}
	if strings.TrimSpace(metadata.Source) != "" {
		entry.Manifest.Metadata.Source = strings.TrimSpace(metadata.Source)
	}
	if strings.TrimSpace(metadata.SkillID) != "" {
		entry.StoredSkillID = strings.TrimSpace(metadata.SkillID)
	}
}

func readStoredMetadata(skillDir string) (*storedSkillMetadata, error) {
	data, err := os.ReadFile(filepath.Join(filepath.FromSlash(skillDir), skillMetadataFileName))
	if err != nil {
		return nil, err
	}
	var metadata storedSkillMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

// resolveInside resolves a relative path within root and validates that the result
// does not escape root via directory traversal.
func resolveInside(root string, relativePath string) (string, error) {
	cleanPath := filepath.Clean(relativePath)
	if filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("absolute path is not allowed")
	}
	target := filepath.Join(root, cleanPath)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("target path escapes skill directory")
	}
	return target, nil
}

// WalkSkillDirs scans root for direct subdirectories containing a SKILL.md file.
// fn receives the directory name and full path to SKILL.md.
// Return filepath.SkipAll from fn to stop walking early.
func WalkSkillDirs(root string, fn func(dirName string, skillMDPath string) error) error {
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read skill root directory %s: %w", root, err)
	}
	for _, rootEntry := range rootEntries {
		subDir := rootEntry.Name()
		info, statErr := os.Stat(filepath.Join(root, subDir))
		if statErr != nil || !info.IsDir() {
			continue
		}
		skillPath := filepath.Join(root, subDir, skillFileName)
		if _, err := os.Stat(skillPath); err != nil {
			continue
		}
		if err := fn(subDir, skillPath); err != nil {
			if errors.Is(err, filepath.SkipAll) {
				return nil
			}
			return err
		}
	}
	return nil
}
