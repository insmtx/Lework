package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/cli"
	"github.com/insmtx/Leros/backend/types"
)

type skillListItem struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	DisplayName     string `json:"display_name,omitempty"`
	Description     string `json:"description,omitempty"`
	Origin          string `json:"origin"`
	Status          string `json:"status"`
	CurrentRevision int    `json:"current_revision"`
}

type skillMutationOutput struct {
	ProjectID  string `json:"project_id"`
	SkillCode  string `json:"skill_code"`
	Associated bool   `json:"associated"`
	Changed    bool   `json:"changed"`
}

func newSkillCommand() *cobra.Command {
	var jsonOutput bool
	var projectID string
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage project skills",
	}
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.PersistentFlags().StringVar(&projectID, "project-id", "", "Project public ID")

	var keyword string
	var offset, limit int
	listCmd := &cobra.Command{
		Use:   "ls",
		Short: "List organization skills or skills associated with a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if offset < 0 || limit <= 0 {
				return fmt.Errorf("--offset must be >= 0 and --limit must be > 0")
			}
			return runSkillList(cmd, jsonOutput, strings.TrimSpace(projectID), keyword, offset, limit)
		},
	}
	listCmd.Flags().StringVar(&keyword, "keyword", "", "Filter by skill code, name, or description")
	listCmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	listCmd.Flags().IntVar(&limit, "limit", 20, "Pagination limit")
	cmd.AddCommand(listCmd)

	addCmd := &cobra.Command{
		Use:   "add <skill_code>",
		Short: "Associate an organization skill with a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillMutation(cmd, jsonOutput, true, strings.TrimSpace(projectID), args[0])
		},
	}
	cmd.AddCommand(addCmd)

	removeCmd := &cobra.Command{
		Use:   "remove <skill_code>",
		Short: "Remove a skill association from a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillMutation(cmd, jsonOutput, false, strings.TrimSpace(projectID), args[0])
		},
	}
	cmd.AddCommand(removeCmd)

	return cmd
}

func runSkillList(
	cmd *cobra.Command,
	jsonOutput bool,
	projectID, keyword string,
	offset, limit int,
) error {
	items := make([]skillListItem, 0)
	if projectID == "" {
		result, err := cli.ListOrganizationPlugins(cmd.Context(), cliServerAddr(), cliAuthToken(), &contract.ListPluginsRequest{
			Kind:    "skill",
			Status:  types.PluginStatusActive,
			Keyword: strings.TrimSpace(keyword),
			Offset:  offset,
			Limit:   limit,
		})
		if err != nil {
			return fmt.Errorf("list organization skills: %w", err)
		}
		for _, skill := range result.Plugins {
			items = append(items, skillListItem{
				Code:            skill.Code,
				Name:            skill.Name,
				DisplayName:     skill.DisplayName,
				Description:     skill.Description,
				Origin:          skill.Origin,
				Status:          skill.Status,
				CurrentRevision: skill.CurrentRevision,
			})
		}
	} else {
		result, err := cli.ListProjectPlugins(cmd.Context(), cliServerAddr(), cliAuthToken(), &contract.ListProjectPluginsRequest{
			PublicID: projectID,
			Kind:     "skill",
			Keyword:  strings.TrimSpace(keyword),
			Offset:   offset,
			Limit:    limit,
		})
		if err != nil {
			return fmt.Errorf("list project skills: %w", err)
		}
		for _, skill := range result {
			items = append(items, skillListItem{
				Code:            skill.Code,
				Name:            skill.Name,
				DisplayName:     skill.DisplayName,
				Description:     skill.Description,
				Origin:          skill.Origin,
				Status:          skill.Status,
				CurrentRevision: skill.CurrentRevision,
			})
		}
	}
	return printSkillList(jsonOutput, items)
}

func runSkillMutation(
	cmd *cobra.Command,
	jsonOutput, add bool,
	projectID, skillCode string,
) error {
	if projectID == "" {
		return fmt.Errorf("--project-id is required")
	}
	skillCode = strings.TrimSpace(skillCode)
	if skillCode == "" {
		return fmt.Errorf("skill code is required")
	}
	req := &contract.UpdateProjectPluginRequest{
		PublicID:   projectID,
		PluginCode: skillCode,
		Kind:       "skill",
	}
	var (
		result *contract.ProjectPluginMutationResult
		err    error
	)
	if add {
		result, err = cli.AddProjectPlugin(cmd.Context(), cliServerAddr(), cliAuthToken(), req)
	} else {
		result, err = cli.RemoveProjectPlugin(cmd.Context(), cliServerAddr(), cliAuthToken(), req)
	}
	if err != nil {
		operation := "remove"
		if add {
			operation = "add"
		}
		return fmt.Errorf("%s project skill: %w", operation, err)
	}
	if jsonOutput {
		return printJSON(skillMutationOutput{
			ProjectID:  result.ProjectID,
			SkillCode:  result.PluginCode,
			Associated: result.Associated,
			Changed:    result.Changed,
		})
	}
	state := "removed from"
	if result.Associated {
		state = "associated with"
	}
	change := "no change"
	if result.Changed {
		change = "changed"
	}
	fmt.Fprintf(os.Stdout, "Skill %s %s project %s (%s).\n", result.PluginCode, state, result.ProjectID, change)
	return nil
}

func printSkillList(jsonOutput bool, items []skillListItem) error {
	if jsonOutput {
		return printJSON(items)
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stdout, "No skills found.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CODE\tNAME\tORIGIN\tREVISION\tSTATUS")
	for _, item := range items {
		name := item.DisplayName
		if strings.TrimSpace(name) == "" {
			name = item.Name
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", item.Code, name, item.Origin, item.CurrentRevision, item.Status)
	}
	return w.Flush()
}
