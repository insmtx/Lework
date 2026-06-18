package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/insmtx/Leros/backend/engines"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/internal/skill/fetch"
	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

var (
	skillJSON  bool
	skillForce bool
	skillYes   bool
	skillLimit int
)

// newSourceRouter 创建包含内置源的 SourceRouter（内置源优先级最高）。
func newSourceRouter() *fetch.SourceRouter {
	return fetch.NewSourceRouterWithSources(
		fetch.NewBuiltinSource(cliServerAddr()),
		fetch.NewUrlSource(),
		fetch.NewGitHubSource(),
		fetch.NewSkillsShSource(),
	)
}

// knownCLISkillDirs 外部 CLI skill 目录，安装后创建 symlink 同步。
var knownCLISkillDirs = []string{
	"~/.claude/skills",
	"~/.agents/skills",
}

func newSkillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage skills from remote sources",
		Long:  "Search, install, list, and uninstall skills.\n\nInstall from GitHub, skills.sh, or direct URL.",
	}

	installCmd := &cobra.Command{
		Use:   "install <identifier>",
		Short: "Install a skill from a remote source",
		Long: `Install a skill by identifier.

Identifier formats:
  <name>                  Short name, resolved via skills.sh exact match
  owner/repo/path         GitHub repository path
  https://.../SKILL.md    Direct URL to a SKILL.md file`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(args[0])
		},
	}

	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search skills across remote sources",
		Long:  `Search for skills across all configured remote sources.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(args[0])
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		Long:  `List skills installed in the local workspace.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList()
		},
	}

	cmd.PersistentFlags().BoolVar(&skillJSON, "json", false, "Output in JSON format")

	installCmd.Flags().BoolVar(&skillForce, "force", false, "Overwrite existing skill")
	installCmd.Flags().BoolVar(&skillYes, "yes", false, "Skip confirmation prompts")

	searchCmd.Flags().IntVar(&skillLimit, "limit", 10, "Maximum number of results")
	searchCmd.Flags().BoolVar(&skillJSON, "json", false, "Output in JSON format")

	uninstallCmd := &cobra.Command{
		Use:   "uninstall <name>",
		Short: "Uninstall an installed skill",
		Long:  `Remove an installed skill by name, cleaning up the skill directory and external symlinks.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(args[0])
		},
	}

	uninstallCmd.Flags().BoolVar(&skillYes, "yes", false, "Skip confirmation prompts")

	cmd.AddCommand(installCmd, searchCmd, listCmd, uninstallCmd)
	return cmd
}

func runInstall(identifier string) error {
	ctx := context.Background()
	router := newSourceRouter()

	// 先尝试 Fetch（BuiltinSource.CanHandle 捕获短名，GitHubSource/UrlSource 捕获完整标识符）。
	bundle, err := router.Fetch(ctx, identifier)
	if err != nil && !strings.Contains(identifier, "/") {
		// 短名 Fetch 失败时，回退到按名称搜索所有源。
		bundle, err = router.ResolveShortName(ctx, identifier)
	}
	if err != nil {
		return fmt.Errorf("resolve skill: %w", err)
	}
	if bundle.TempDir != "" {
		defer os.RemoveAll(bundle.TempDir)
	}

	meta := bundle.Meta

	skillsDir, err := leros.SkillsDir()
	if err != nil {
		return fmt.Errorf("resolve skills dir: %w", err)
	}
	store, err := skillstore.NewSkillStore(skillsDir)
	if err != nil {
		return fmt.Errorf("create skill store: %w", err)
	}

	// 将 bundle.Files (map[string][]byte) 转为 map[string]string。
	files := make(map[string]string, len(bundle.Files))
	for relPath, data := range bundle.Files {
		files[relPath] = string(data)
	}

	result, err := store.Install(ctx, skillstore.InstallRequest{
		Name:    meta.Name,
		Content: string(bundle.Content),
		Files:   files,
		Force:   skillForce,
	})
	if err != nil {
		return fmt.Errorf("install skill: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("install skill: %s", result.Error)
	}

	// 同步到外部 CLI skill 目录。
	if err := engines.EnsureExternalSkillLink(meta.Name, knownCLISkillDirs); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: sync external links: %v\n", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.Encode(map[string]any{"installed": true, "name": meta.Name})
	return nil
}

func runSearch(query string) error {
	ctx := context.Background()
	router := newSourceRouter()

	results, err := router.Search(ctx, query, skillLimit)
	if err != nil {
		return fmt.Errorf("search skills: %w", err)
	}

	if skillJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if results == nil {
			results = []fetch.SkillMeta{}
		}
		return enc.Encode(results)
	}

	if len(results) == 0 {
		fmt.Printf("No skills found matching %q.\n", query)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tIDENTIFIER\tSOURCE\tTRUST\tDESCRIPTION")
	for _, r := range results {
		desc := r.Description
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, r.Identifier, r.Source, r.TrustLevel, desc)
	}
	w.Flush()

	fmt.Fprintf(os.Stderr, "\nFound %d result(s).\n", len(results))
	return nil
}

func runList() error {
	summaries, err := skillcatalog.List()
	if err != nil {
		return fmt.Errorf("list skills: %w", err)
	}

	if skillJSON {
		type listItem struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
			Source      string `json:"source"`
			Trust       string `json:"trust"`
		}
		items := make([]listItem, 0, len(summaries))
		for _, s := range summaries {
			items = append(items, listItem{
				Name:        s.Name,
				Description: s.Description,
				Category:    s.Category,
				Source:      s.Source,
				Trust:       s.Trust,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if items == nil {
			items = []listItem{}
		}
		return enc.Encode(items)
	}

	if len(summaries) == 0 {
		fmt.Println("No skills installed.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tCATEGORY\tSOURCE\tTRUST")
	for _, s := range summaries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.Category, s.Source, s.Trust)
	}
	w.Flush()

	fmt.Fprintf(os.Stderr, "\n%d skill(s) installed.\n", len(summaries))
	return nil
}

func runUninstall(name string) error {
	ctx := context.Background()

	skillsDir, err := leros.SkillsDir()
	if err != nil {
		return fmt.Errorf("resolve skills dir: %w", err)
	}
	store, err := skillstore.NewSkillStore(skillsDir)
	if err != nil {
		return fmt.Errorf("create skill store: %w", err)
	}

	result, err := store.Delete(ctx, skillstore.DeleteRequest{Name: name})
	if err != nil {
		return fmt.Errorf("uninstall skill: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("uninstall skill: %s", result.Error)
	}

	// Remove external CLI symlinks
	if err := engines.RemoveExternalSkillLink(name, knownCLISkillDirs); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: remove external links: %v\n", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]any{"uninstalled": true, "name": name})
	return nil
}
