// Package local implements Leros built-in file-backed memory.
package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

const (
	// TargetMemory stores worker facts, environment notes, and durable lessons.
	TargetMemory = "memory"
	// TargetUser stores user preferences and profile facts.
	TargetUser = "user"
	// TargetProjectMemory stores project-level persistent memory.
	TargetProjectMemory = "project_memory"
)

const (
	// EntryDelimiter separates individual memory entries within a file.
	EntryDelimiter         = "\n§\n"
	defaultMemoryCharLimit = 2200
	defaultUserCharLimit   = 1375
)

// Store persists compact curated memories to USER.md and MEMORY.md.
type Store struct {
	rootDir         string
	memoryCharLimit int
	userCharLimit   int
}

// Options configures a Store.
type Options struct {
	RootDir         string
	MemoryCharLimit int
	UserCharLimit   int
}

// EntrySet is the live memory state for one target.
type EntrySet struct {
	Target     string   `json:"target"`
	Entries    []string `json:"entries"`
	Usage      string   `json:"usage"`
	EntryCount int      `json:"entry_count"`
}

// Result is returned from mutating memory operations.
type Result struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message,omitempty"`
	Target     string   `json:"target,omitempty"`
	Entries    []string `json:"entries,omitempty"`
	Usage      string   `json:"usage,omitempty"`
	EntryCount int      `json:"entry_count,omitempty"`
	Error      string   `json:"error,omitempty"`
	Matches    []string `json:"matches,omitempty"`
	MemoryRoot string   `json:"memory_root,omitempty"`
	MemoryFile string   `json:"memory_file,omitempty"`
}

// NewStore creates a file-backed memory store.
func NewStore(opts Options) (*Store, error) {
	rootDir := strings.TrimSpace(opts.RootDir)
	var err error
	if rootDir == "" {
		rootDir, err = DefaultMemoryRoot()
		if err != nil {
			return nil, err
		}
	}
	rootDir, err = filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve memory root: %w", err)
	}

	memoryLimit := opts.MemoryCharLimit
	if memoryLimit <= 0 {
		memoryLimit = defaultMemoryCharLimit
	}
	userLimit := opts.UserCharLimit
	if userLimit <= 0 {
		userLimit = defaultUserCharLimit
	}

	return &Store{
		rootDir:         rootDir,
		memoryCharLimit: memoryLimit,
		userCharLimit:   userLimit,
	}, nil
}

// DefaultMemoryRoot returns the workspace memory directory.
func DefaultMemoryRoot() (string, error) {
	return leros.MemoryDir()
}

// RootDir returns the memory directory containing USER.md and MEMORY.md.
func (s *Store) RootDir() string {
	if s == nil {
		return ""
	}
	return s.rootDir
}

// Add appends a compact memory entry.
func (s *Store) Add(ctx context.Context, target string, content string) (*Result, error) {
	return s.mutate(ctx, target, func(entries []string, limit int) ([]string, *Result) {
		content = strings.TrimSpace(content)
		if content == "" {
			return entries, failure("Content cannot be empty.")
		}
		if scanErr := scanContent(content); scanErr != "" {
			return entries, failure(scanErr)
		}
		for _, entry := range entries {
			if entry == content {
				return entries, nil
			}
		}
		next := append(append([]string{}, entries...), content)
		if len(joinEntries(next)) > limit {
			return entries, failure(fmt.Sprintf(
				"Memory at %s. Adding this entry (%d chars) would exceed the limit.",
				usage(entries, limit), len(content),
			))
		}
		return next, nil
	}, "Entry added.")
}

// Replace replaces the entry containing oldText with newContent.
func (s *Store) Replace(ctx context.Context, target string, oldText string, newContent string) (*Result, error) {
	return s.mutate(ctx, target, func(entries []string, limit int) ([]string, *Result) {
		oldText = strings.TrimSpace(oldText)
		newContent = strings.TrimSpace(newContent)
		if oldText == "" {
			return entries, failure("old_text cannot be empty.")
		}
		if newContent == "" {
			return entries, failure("content cannot be empty. Use remove to delete entries.")
		}
		if scanErr := scanContent(newContent); scanErr != "" {
			return entries, failure(scanErr)
		}

		matches := matchingEntries(entries, oldText)
		if len(matches) == 0 {
			return entries, failure(fmt.Sprintf("No entry matched %q.", oldText))
		}
		if !matchesUniquely(entries, matches) {
			return entries, &Result{
				Success: false,
				Error:   fmt.Sprintf("Multiple entries matched %q. Be more specific.", oldText),
				Matches: previews(entries, matches),
			}
		}

		next := append([]string{}, entries...)
		next[matches[0]] = newContent
		if len(joinEntries(next)) > limit {
			return entries, failure(fmt.Sprintf(
				"Replacement would put memory at %s. Shorten the new content or remove other entries first.",
				usage(next, limit),
			))
		}
		return next, nil
	}, "Entry replaced.")
}

// Remove deletes the entry containing oldText.
func (s *Store) Remove(ctx context.Context, target string, oldText string) (*Result, error) {
	return s.mutate(ctx, target, func(entries []string, _ int) ([]string, *Result) {
		oldText = strings.TrimSpace(oldText)
		if oldText == "" {
			return entries, failure("old_text cannot be empty.")
		}
		matches := matchingEntries(entries, oldText)
		if len(matches) == 0 {
			return entries, failure(fmt.Sprintf("No entry matched %q.", oldText))
		}
		if !matchesUniquely(entries, matches) {
			return entries, &Result{
				Success: false,
				Error:   fmt.Sprintf("Multiple entries matched %q. Be more specific.", oldText),
				Matches: previews(entries, matches),
			}
		}

		next := append([]string{}, entries[:matches[0]]...)
		next = append(next, entries[matches[0]+1:]...)
		return next, nil
	}, "Entry removed.")
}

// Load reads one target without mutating it.
func (s *Store) Load(ctx context.Context, target string) (*EntrySet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	entries, err := ReadEntries(s.pathFor(target))
	if err != nil {
		return nil, err
	}
	return s.entrySet(target, entries), nil
}

// BuildPromptBlock renders the current memory snapshot for system prompt injection.
func (s *Store) BuildPromptBlock(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s == nil {
		return "", nil
	}

	userEntries, err := ReadEntries(s.pathFor(TargetUser))
	if err != nil {
		return "", err
	}
	memoryEntries, err := ReadEntries(s.pathFor(TargetMemory))
	if err != nil {
		return "", err
	}
	if len(userEntries) == 0 && len(memoryEntries) == 0 {
		return "", nil
	}

	sections := []string{
		"<memory-context>",
		"[System note: The following is persistent memory, not new user input.]",
	}
	if len(userEntries) > 0 {
		sections = append(sections, "", "User Memory:", bulletList(userEntries))
	}
	if len(memoryEntries) > 0 {
		sections = append(sections, "", "Worker Memory:", bulletList(memoryEntries))
	}
	sections = append(sections, "</memory-context>")
	return strings.Join(sections, "\n"), nil
}

func (s *Store) mutate(ctx context.Context, target string, update func([]string, int) ([]string, *Result), successMessage string) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("memory store is nil")
	}
	target, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	path := s.pathFor(target)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}

	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := ReadEntries(path)
	if err != nil {
		return nil, err
	}

	next, failed := update(entries, s.limitFor(target))
	if failed != nil {
		failed.Target = target
		failed.MemoryRoot = s.rootDir
		failed.MemoryFile = path
		return failed, nil
	}
	if err := writeEntriesAtomic(path, next); err != nil {
		return nil, err
	}

	result := s.result(target, next, successMessage)
	result.MemoryRoot = s.rootDir
	result.MemoryFile = path
	return result, nil
}

func (s *Store) pathFor(target string) string {
	fileName := "MEMORY.md"
	switch target {
	case TargetUser:
		fileName = "USER.md"
	case TargetProjectMemory:
		fileName = "project_memory.md"
	}
	return filepath.Join(s.rootDir, fileName)
}

func (s *Store) limitFor(target string) int {
	if target == TargetUser {
		return s.userCharLimit
	}
	return s.memoryCharLimit
}

func (s *Store) entrySet(target string, entries []string) *EntrySet {
	return &EntrySet{
		Target:     target,
		Entries:    append([]string{}, entries...),
		Usage:      usage(entries, s.limitFor(target)),
		EntryCount: len(entries),
	}
}

func (s *Store) result(target string, entries []string, message string) *Result {
	set := s.entrySet(target, entries)
	return &Result{
		Success:    true,
		Message:    message,
		Target:     set.Target,
		Entries:    set.Entries,
		Usage:      set.Usage,
		EntryCount: set.EntryCount,
	}
}

func normalizeTarget(target string) (string, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	switch target {
	case TargetUser, TargetMemory, TargetProjectMemory:
		return target, nil
	default:
		return "", fmt.Errorf("invalid memory target %q: use user, memory, or project_memory", target)
	}
}

// ReadEntries reads and parses memory entries from a file.
func ReadEntries(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read memory file %s: %w", path, err)
	}
	raw := strings.TrimSpace(string(content))
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, EntryDelimiter)
	entries := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		entries = append(entries, entry)
	}
	return entries, nil
}

func writeEntriesAtomic(path string, entries []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp memory file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.WriteString(joinEntries(entries)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp memory file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp memory file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp memory file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace memory file: %w", err)
	}
	return nil
}

func lockFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open memory lock: %w", err)
	}
	if err := lockOpenFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock memory file: %w", err)
	}
	return func() {
		_ = unlockOpenFile(file)
		_ = file.Close()
	}, nil
}

func joinEntries(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	return strings.Join(entries, EntryDelimiter)
}

func usage(entries []string, limit int) string {
	current := len(joinEntries(entries))
	pct := 0
	if limit > 0 {
		pct = min(100, current*100/limit)
	}
	return fmt.Sprintf("%d%% — %d/%d chars", pct, current, limit)
}

func failure(message string) *Result {
	return &Result{
		Success: false,
		Error:   message,
	}
}

func matchingEntries(entries []string, oldText string) []int {
	matches := make([]int, 0, 1)
	for i, entry := range entries {
		if strings.Contains(entry, oldText) {
			matches = append(matches, i)
		}
	}
	return matches
}

func matchesUniquely(entries []string, matches []int) bool {
	if len(matches) <= 1 {
		return true
	}
	first := entries[matches[0]]
	for _, idx := range matches[1:] {
		if entries[idx] != first {
			return false
		}
	}
	return true
}

func previews(entries []string, matches []int) []string {
	result := make([]string, 0, len(matches))
	for _, idx := range matches {
		entry := entries[idx]
		if len(entry) > 80 {
			entry = entry[:80] + "..."
		}
		result = append(result, entry)
	}
	return result
}

func bulletList(entries []string) string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.ReplaceAll(strings.TrimSpace(entry), "\n", "\n  ")
		lines = append(lines, "- "+entry)
	}
	return strings.Join(lines, "\n")
}

func scanContent(content string) string {
	for _, ch := range []rune{'\u200b', '\u200c', '\u200d', '\u2060', '\ufeff', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e'} {
		if strings.ContainsRune(content, ch) {
			return fmt.Sprintf("Blocked: content contains invisible unicode character U+%04X.", ch)
		}
	}

	lower := strings.ToLower(content)
	blockedPatterns := []struct {
		needle string
		label  string
	}{
		{"ignore previous instructions", "prompt_injection"},
		{"ignore all instructions", "prompt_injection"},
		{"disregard your instructions", "prompt_injection"},
		{"system prompt override", "prompt_injection"},
		{"do not tell the user", "deception"},
		{"authorized_keys", "ssh_backdoor"},
		{"cat .env", "secret_exfiltration"},
		{"cat ~/.ssh", "secret_exfiltration"},
		{"curl ${", "secret_exfiltration"},
		{"wget ${", "secret_exfiltration"},
	}
	for _, pattern := range blockedPatterns {
		if strings.Contains(lower, pattern.needle) {
			return fmt.Sprintf("Blocked: content matches threat pattern %q.", pattern.label)
		}
	}
	return ""
}
