package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/cli"
	"github.com/insmtx/Leros/backend/types"
)

const defaultAutomationTimezone = "Asia/Shanghai"

type automationScheduleOptions struct {
	mode            string
	timezone        string
	preset          string
	minute          int
	hour            int
	daysOfWeek      string
	daysOfMonth     string
	intervalMinutes int
}

type automationCreateOptions struct {
	name      string
	prompt    string
	status    string
	projectID string
	schedule  automationScheduleOptions
}

type automationUpdateOptions struct {
	name       string
	prompt     string
	status     string
	projectID  string
	newProject bool
	schedule   automationScheduleOptions
}

func newAutomationCommand() *cobra.Command {
	var jsonOutput bool
	var targetUserID uint
	cmd := &cobra.Command{
		Use:   "automation",
		Short: "Manage automations",
	}
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.PersistentFlags().UintVar(&targetUserID, "user-id", 0, "Target user ID")

	var listKeyword, listStatus, listMode string
	var listOffset, listLimit int
	listCmd := &cobra.Command{
		Use:   "ls",
		Short: "List automations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			userID, err := resolveAutomationTargetUserID(targetUserID)
			if err != nil {
				return err
			}
			return runAutomationList(cmd, jsonOutput, listKeyword, listStatus, listMode, listOffset, listLimit, userID)
		},
	}
	listCmd.Flags().StringVar(&listKeyword, "keyword", "", "Filter by name or instruction keyword")
	listCmd.Flags().StringVar(&listStatus, "status", "", "Filter by status: enabled or paused")
	listCmd.Flags().StringVar(&listMode, "mode", "", "Filter by mode: calendar or interval")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "Pagination offset")
	listCmd.Flags().IntVar(&listLimit, "limit", 20, "Pagination limit")
	cmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <automation_id>",
		Short: "Get automation details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID, err := resolveAutomationTargetUserID(targetUserID)
			if err != nil {
				return err
			}
			return runAutomationGet(cmd, jsonOutput, args[0], userID)
		},
	}
	cmd.AddCommand(getCmd)

	createOptions := automationCreateOptions{}
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create an automation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			userID, err := resolveAutomationTargetUserID(targetUserID)
			if err != nil {
				return err
			}
			return runAutomationCreate(cmd, jsonOutput, createOptions, userID)
		},
	}
	bindAutomationCreateFlags(createCmd, &createOptions)
	cmd.AddCommand(createCmd)

	updateOptions := automationUpdateOptions{}
	updateCmd := &cobra.Command{
		Use:   "update <automation_id>",
		Short: "Update an automation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID, err := resolveAutomationTargetUserID(targetUserID)
			if err != nil {
				return err
			}
			return runAutomationUpdate(cmd, jsonOutput, args[0], updateOptions, userID)
		},
	}
	bindAutomationUpdateFlags(updateCmd, &updateOptions)
	cmd.AddCommand(updateCmd)

	statusCmd := &cobra.Command{
		Use:   "status <automation_id> <enabled|paused>",
		Short: "Change automation status",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := parseAutomationStatus(args[1])
			if err != nil {
				return err
			}
			userID, err := resolveAutomationTargetUserID(targetUserID)
			if err != nil {
				return err
			}
			result, err := cli.UpdateAutomation(cmd.Context(), cliServerAddr(), cliAuthToken(), args[0], &contract.UpdateAutomationRequest{Enabled: &status}, userID)
			if err != nil {
				return fmt.Errorf("update automation status: %w", err)
			}
			return printAutomation(jsonOutput, result)
		},
	}
	cmd.AddCommand(statusCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <automation_id>",
		Short: "Delete an automation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID, err := resolveAutomationTargetUserID(targetUserID)
			if err != nil {
				return err
			}
			if err := cli.DeleteAutomation(cmd.Context(), cliServerAddr(), cliAuthToken(), args[0], userID); err != nil {
				return fmt.Errorf("delete automation: %w", err)
			}
			if jsonOutput {
				return printJSON(struct {
					PublicID string `json:"public_id"`
					Deleted  bool   `json:"deleted"`
				}{PublicID: args[0], Deleted: true})
			}
			fmt.Fprintf(os.Stdout, "Automation %s deleted.\n", args[0])
			return nil
		},
	}
	cmd.AddCommand(deleteCmd)

	return cmd
}

func resolveAutomationTargetUserID(explicit uint) (*uint, error) {
	if explicit > 0 {
		return &explicit, nil
	}
	return nil, nil
}

func bindAutomationCreateFlags(cmd *cobra.Command, options *automationCreateOptions) {
	cmd.Flags().StringVar(&options.name, "name", "", "Automation name (required)")
	cmd.Flags().StringVar(&options.prompt, "prompt", "", "Instruction to execute (required)")
	cmd.Flags().StringVar(&options.status, "status", "", "Status: enabled or paused (required)")
	cmd.Flags().StringVar(&options.projectID, "project-id", "", "Existing project public ID")
	bindAutomationScheduleFlags(cmd, &options.schedule)
}

func bindAutomationUpdateFlags(cmd *cobra.Command, options *automationUpdateOptions) {
	cmd.Flags().StringVar(&options.name, "name", "", "New automation name")
	cmd.Flags().StringVar(&options.prompt, "prompt", "", "New instruction")
	cmd.Flags().StringVar(&options.status, "status", "", "New status: enabled or paused")
	cmd.Flags().StringVar(&options.projectID, "project-id", "", "New project public ID")
	cmd.Flags().BoolVar(&options.newProject, "new-project", false, "Switch to a new default project")
	bindAutomationScheduleFlags(cmd, &options.schedule)
}

func bindAutomationScheduleFlags(cmd *cobra.Command, options *automationScheduleOptions) {
	cmd.Flags().StringVar(&options.mode, "mode", "", "Schedule mode: calendar or interval")
	cmd.Flags().StringVar(&options.timezone, "timezone", "", "IANA timezone (default: Asia/Shanghai)")
	cmd.Flags().StringVar(&options.preset, "preset", "", "Calendar preset: hourly, daily, weekly, or monthly")
	cmd.Flags().IntVar(&options.minute, "minute", 0, "Minute of hour (0-59)")
	cmd.Flags().IntVar(&options.hour, "hour", 0, "Hour of day (0-23)")
	cmd.Flags().StringVar(&options.daysOfWeek, "days-of-week", "", "Weekly days, comma-separated (0=Sunday through 6=Saturday)")
	cmd.Flags().StringVar(&options.daysOfMonth, "days-of-month", "", "Monthly dates, comma-separated (1-31)")
	cmd.Flags().IntVar(&options.intervalMinutes, "interval-minutes", 0, "Interval in minutes (minimum 5)")
}

func runAutomationList(cmd *cobra.Command, jsonOutput bool, keyword, status, mode string, offset, limit int, targetUserID *uint) error {
	if offset < 0 || limit <= 0 {
		return fmt.Errorf("--offset must be >= 0 and --limit must be > 0")
	}
	var req contract.ListAutomationsRequest
	req.Offset = offset
	req.Limit = limit
	if keyword != "" {
		req.Keyword = &keyword
	}
	if status != "" {
		enabled, err := parseAutomationStatus(status)
		if err != nil {
			return err
		}
		req.Enabled = &enabled
	}
	if mode != "" {
		if mode != string(types.AutomationScheduleModeCalendar) && mode != string(types.AutomationScheduleModeInterval) {
			return fmt.Errorf("invalid --mode %q; valid values: calendar, interval", mode)
		}
		req.ScheduleMode = &mode
	}
	req.Fill()

	result, err := cli.ListAutomations(cmd.Context(), cliServerAddr(), cliAuthToken(), &req, targetUserID)
	if err != nil {
		return fmt.Errorf("list automations: %w", err)
	}
	if jsonOutput {
		return printJSON(result.Items)
	}
	printAutomationList(result)
	return nil
}

func runAutomationGet(cmd *cobra.Command, jsonOutput bool, publicID string, targetUserID *uint) error {
	result, err := cli.GetAutomation(cmd.Context(), cliServerAddr(), cliAuthToken(), publicID, targetUserID)
	if err != nil {
		return fmt.Errorf("get automation: %w", err)
	}
	return printAutomation(jsonOutput, result)
}

func runAutomationCreate(cmd *cobra.Command, jsonOutput bool, options automationCreateOptions, targetUserID *uint) error {
	name := strings.TrimSpace(options.name)
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	prompt := strings.TrimSpace(options.prompt)
	if prompt == "" {
		return fmt.Errorf("--prompt is required")
	}
	enabled, err := parseRequiredAutomationStatus(options.status)
	if err != nil {
		return err
	}
	schedule, timezone, err := buildAutomationSchedule(options.schedule, true)
	if err != nil {
		return err
	}
	result, err := cli.CreateAutomation(cmd.Context(), cliServerAddr(), cliAuthToken(), &contract.CreateAutomationRequest{
		Name:            name,
		Instruction:     prompt,
		Enabled:         &enabled,
		ScheduleMode:    schedule.Mode,
		Schedule:        schedule,
		Timezone:        timezone,
		ProjectPublicID: strings.TrimSpace(options.projectID),
	}, targetUserID)
	if err != nil {
		return fmt.Errorf("create automation: %w", err)
	}
	return printAutomation(jsonOutput, result)
}

func runAutomationUpdate(cmd *cobra.Command, jsonOutput bool, publicID string, options automationUpdateOptions, targetUserID *uint) error {
	req := &contract.UpdateAutomationRequest{}
	if cmd.Flags().Changed("name") {
		name := strings.TrimSpace(options.name)
		if name == "" {
			return fmt.Errorf("--name cannot be empty")
		}
		req.Name = name
	}
	if cmd.Flags().Changed("prompt") {
		prompt := strings.TrimSpace(options.prompt)
		if prompt == "" {
			return fmt.Errorf("--prompt cannot be empty")
		}
		req.Instruction = &prompt
	}
	if cmd.Flags().Changed("status") {
		enabled, err := parseRequiredAutomationStatus(options.status)
		if err != nil {
			return err
		}
		req.Enabled = &enabled
	}
	if cmd.Flags().Changed("project-id") && options.newProject {
		return fmt.Errorf("--project-id and --new-project cannot be used together")
	}
	if cmd.Flags().Changed("project-id") {
		projectID := strings.TrimSpace(options.projectID)
		req.ProjectPublicID = &projectID
	}
	if options.newProject {
		projectID := ""
		req.ProjectPublicID = &projectID
	}

	if hasAutomationScheduleChange(cmd) {
		schedule, _, err := buildAutomationSchedule(options.schedule, false)
		if err != nil {
			return err
		}
		req.ScheduleMode = &schedule.Mode
		req.Schedule = schedule
	}
	if cmd.Flags().Changed("timezone") {
		timezone := strings.TrimSpace(options.schedule.timezone)
		if timezone == "" {
			return fmt.Errorf("--timezone cannot be empty")
		}
		if _, err := time.LoadLocation(timezone); err != nil {
			return fmt.Errorf("invalid timezone %q: %w", timezone, err)
		}
		req.Timezone = &timezone
	}
	if lenUpdateFields(req) == 0 {
		return fmt.Errorf("no fields to update; use --name, --prompt, --status, schedule flags, --timezone, --project-id, or --new-project")
	}

	result, err := cli.UpdateAutomation(cmd.Context(), cliServerAddr(), cliAuthToken(), publicID, req, targetUserID)
	if err != nil {
		return fmt.Errorf("update automation: %w", err)
	}
	return printAutomation(jsonOutput, result)
}

func hasAutomationScheduleChange(cmd *cobra.Command) bool {
	for _, name := range []string{"mode", "preset", "minute", "hour", "days-of-week", "days-of-month", "interval-minutes"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func lenUpdateFields(req *contract.UpdateAutomationRequest) int {
	count := 0
	if req.Name != "" {
		count++
	}
	if req.Instruction != nil || req.Enabled != nil || req.ScheduleMode != nil || req.Schedule != nil || req.Timezone != nil || req.ProjectPublicID != nil {
		count++
	}
	return count
}

func buildAutomationSchedule(options automationScheduleOptions, create bool) (*contract.AutomationScheduleInput, string, error) {
	mode := strings.TrimSpace(options.mode)
	if mode != string(types.AutomationScheduleModeCalendar) && mode != string(types.AutomationScheduleModeInterval) {
		return nil, "", fmt.Errorf("--mode must be calendar or interval")
	}
	timezone := strings.TrimSpace(options.timezone)
	if create && timezone == "" {
		timezone = defaultAutomationTimezone
	}
	if timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			return nil, "", fmt.Errorf("invalid timezone %q: %w", timezone, err)
		}
	}
	form := &contract.AutomationScheduleInput{Mode: mode, Timezone: timezone}

	switch mode {
	case string(types.AutomationScheduleModeCalendar):
		if options.preset != "hourly" && options.preset != "daily" && options.preset != "weekly" && options.preset != "monthly" {
			return nil, "", fmt.Errorf("--preset must be hourly, daily, weekly, or monthly")
		}
		if options.minute < 0 || options.minute > 59 {
			return nil, "", fmt.Errorf("--minute must be between 0 and 59")
		}
		calendar := &types.AutomationCalendarConfig{Preset: options.preset, Minute: options.minute}
		if options.preset == "hourly" {
			if options.hour != 0 || options.daysOfWeek != "" || options.daysOfMonth != "" {
				return nil, "", fmt.Errorf("--hour, --days-of-week, and --days-of-month are not valid with --preset hourly")
			}
		} else {
			if options.hour < 0 || options.hour > 23 {
				return nil, "", fmt.Errorf("--hour must be between 0 and 23")
			}
			calendar.Hour = options.hour
		}
		if options.preset == "weekly" {
			days, err := parseAutomationDays(options.daysOfWeek, 0, 6)
			if err != nil {
				return nil, "", fmt.Errorf("--days-of-week: %w", err)
			}
			calendar.DaysOfWeek = days
		} else if options.daysOfWeek != "" {
			return nil, "", fmt.Errorf("--days-of-week is only valid with --preset weekly")
		}
		if options.preset == "monthly" {
			days, err := parseAutomationDays(options.daysOfMonth, 1, 31)
			if err != nil {
				return nil, "", fmt.Errorf("--days-of-month: %w", err)
			}
			calendar.DaysOfMonth = days
		} else if options.daysOfMonth != "" {
			return nil, "", fmt.Errorf("--days-of-month is only valid with --preset monthly")
		}
		form.Calendar = calendar
	case string(types.AutomationScheduleModeInterval):
		if options.preset != "" || options.hour != 0 || options.minute != 0 || options.daysOfWeek != "" || options.daysOfMonth != "" {
			return nil, "", fmt.Errorf("calendar flags are not valid with --mode interval")
		}
		if options.intervalMinutes < 5 {
			return nil, "", fmt.Errorf("--interval-minutes must be at least 5")
		}
		form.Interval = &contract.AutomationIntervalInput{IntervalMinutes: options.intervalMinutes}
	}
	return form, timezone, nil
}

func parseAutomationDays(raw string, min, max int) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("a non-empty comma-separated value is required")
	}
	seen := make(map[int]bool)
	result := make([]int, 0)
	for _, part := range strings.Split(raw, ",") {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < min || value > max || seen[value] {
			return nil, fmt.Errorf("values must be unique integers between %d and %d", min, max)
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func parseRequiredAutomationStatus(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, fmt.Errorf("--status is required")
	}
	return parseAutomationStatus(raw)
}

func parseAutomationStatus(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "enabled":
		return true, nil
	case "paused":
		return false, nil
	default:
		return false, fmt.Errorf("invalid status %q; valid values: enabled, paused", raw)
	}
}

func printAutomationList(list *contract.AutomationList) {
	if len(list.Items) == 0 {
		fmt.Fprintln(os.Stdout, "No automations found.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PUBLIC_ID\tNAME\tSTATUS\tMODE\tSCHEDULE\tTIMEZONE\tNEXT_RUN_AT\tPROJECT_ID")
	for _, automation := range list.Items {
		status := "paused"
		if automation.Enabled {
			status = "enabled"
		}
		nextRunAt := ""
		if automation.NextRunAt != nil {
			nextRunAt = formatAutomationTime(*automation.NextRunAt, automation.Timezone)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			automation.PublicID, automation.Name, status, automation.ScheduleMode,
			automation.Summary, automation.Timezone, nextRunAt, automation.ProjectPublicID)
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "\nTotal: %d, Offset: %d, Limit: %d\n", list.Total, list.Offset, list.Limit)
}

func printAutomation(jsonOutput bool, automation *contract.Automation) error {
	if jsonOutput {
		return printJSON(automation)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	status := "paused"
	if automation.Enabled {
		status = "enabled"
	}
	fmt.Fprintf(w, "PublicID:\t%s\n", automation.PublicID)
	fmt.Fprintf(w, "Name:\t%s\n", automation.Name)
	fmt.Fprintf(w, "Prompt:\t%s\n", automation.Instruction)
	fmt.Fprintf(w, "Status:\t%s\n", status)
	fmt.Fprintf(w, "Mode:\t%s\n", automation.ScheduleMode)
	fmt.Fprintf(w, "Schedule:\t%s\n", automation.Summary)
	fmt.Fprintf(w, "Timezone:\t%s\n", automation.Timezone)
	if automation.NextRunAt != nil {
		fmt.Fprintf(w, "NextRunAt:\t%s\n", formatAutomationTime(*automation.NextRunAt, automation.Timezone))
	}
	if automation.ProjectPublicID != "" {
		fmt.Fprintf(w, "ProjectID:\t%s\n", automation.ProjectPublicID)
	}
	fmt.Fprintf(w, "CreatedAt:\t%s\n", automation.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "UpdatedAt:\t%s\n", automation.UpdatedAt.Format(time.RFC3339))
	w.Flush()
	return nil
}

func formatAutomationTime(value time.Time, timezone string) string {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return value.UTC().Format(time.RFC3339)
	}
	return value.In(loc).Format(time.RFC3339)
}

func printJSON(value interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
