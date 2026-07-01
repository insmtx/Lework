package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/lifecycle"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/cli"
)

var (
	taskJSON       bool
	taskKeyword    string
	taskStatus     string
	taskProjectID  string
	taskType       string
	taskAssigneeID uint
	taskOffset     int
	taskLimit      int
)

type taskDetailOutput struct {
	Task         *contract.Task                   `json:"task,omitempty"`
	Project      *contract.Project                `json:"project,omitempty"`
	Files        []contract.FileTreeNode          `json:"files,omitempty"`
	Assignee     *contract.DigitalAssistantDetail `json:"assignee,omitempty"`
	OwnerName    string                           `json:"owner_name,omitempty"`
	AssigneeName string                           `json:"assignee_name,omitempty"`
}

func newTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
		Long:  `Manage tasks in the Leros platform.`,
	}

	lsCmd := &cobra.Command{
		Use:   "ls",
		Short: "List tasks",
		Long:  `List all tasks with optional filtering.`,
		Run: func(cmd *cobra.Command, args []string) {
			go func() {
				req := &contract.ListTasksRequest{
					Pagination: contract.ListTasksRequest{}.Pagination,
				}
				req.Offset = taskOffset
				req.Limit = taskLimit
				req.Fill()

				if taskKeyword != "" {
					req.Keyword = &taskKeyword
				}
				if taskStatus != "" {
					req.Status = &taskStatus
				}
				if cmd.Flags().Changed("project-id") {
					req.ProjectID = &taskProjectID
				}
				if taskType != "" {
					req.TaskType = &taskType
				}
				if cmd.Flags().Changed("assignee-id") {
					req.AssigneeID = &taskAssigneeID
				}

				result, err := cli.ListTasks(lifecycle.Std().Context(), cliServerAddr(), cliAuthToken(), req)
				if err != nil {
					logs.Errorf("list tasks: %v", err)
					lifecycle.Std().Exit()
					return
				}
				printTasks(result)
				lifecycle.Std().Exit()
			}()
			lifecycle.Std().WaitExit()
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <task_id>",
		Short: "Get task details",
		Long:  `Get detailed information about a specific task by its public ID.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			go func() {
				ctx := lifecycle.Std().Context()
				publicID := args[0]

				task, err := cli.GetTask(ctx, cliServerAddr(), cliAuthToken(), publicID)
				if err != nil {
					logs.Errorf("get task: %v", err)
					lifecycle.Std().Exit()
					return
				}

				out := taskDetailOutput{Task: task}

				if task.OwnerID > 0 {
					out.OwnerName = cli.ResolveUserName(ctx, cliServerAddr(), cliAuthToken(), task.OwnerID)
				}

				if task.ProjectID != "" {
					prj, err := cli.GetProject(ctx, cliServerAddr(), cliAuthToken(), task.ProjectID)
					if err != nil {
						logs.Warnf("get project: %v", err)
					} else {
						out.Project = prj
					}
				}

				projectID := task.ProjectID
				if projectID != "" {
					files, err := cli.ListProjectFiles(ctx, cliServerAddr(), cliAuthToken(), projectID, "artifact", publicID)
					if err != nil {
						logs.Warnf("list task files: %v", err)
					} else {
						out.Files = files
					}
				}

				if task.AssigneeID != nil && *task.AssigneeID > 0 {
					ast, err := cli.GetDigitalAssistantByID(ctx, cliServerAddr(), cliAuthToken(), *task.AssigneeID)
					if err != nil {
						out.AssigneeName = cli.ResolveUserName(ctx, cliServerAddr(), cliAuthToken(), *task.AssigneeID)
					} else {
						out.Assignee = ast
					}
				}

				printTaskDetail(&out)
				lifecycle.Std().Exit()
			}()
			lifecycle.Std().WaitExit()
		},
	}

	cmd.PersistentFlags().BoolVar(&taskJSON, "json", false, "Output in JSON format")

	lsCmd.Flags().StringVar(&taskKeyword, "keyword", "", "Filter by title/description keyword")
	lsCmd.Flags().StringVar(&taskStatus, "status", "", "Filter by status")
	lsCmd.Flags().StringVar(&taskProjectID, "project-id", "", "Filter by project ID")
	lsCmd.Flags().StringVar(&taskType, "type", "", "Filter by task type")
	lsCmd.Flags().UintVar(&taskAssigneeID, "assignee-id", 0, "Filter by assignee ID")
	lsCmd.Flags().IntVar(&taskOffset, "offset", 0, "Pagination offset")
	lsCmd.Flags().IntVar(&taskLimit, "limit", 20, "Pagination limit")

	cmd.AddCommand(lsCmd)
	cmd.AddCommand(getCmd)
	return cmd
}

func printTasks(list *contract.TaskList) {
	if taskJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(list.Items)
		return
	}

	if len(list.Items) == 0 {
		fmt.Println("No tasks found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PUBLIC_ID\tTITLE\tSTATUS\tTYPE\tPROJECT_ID\tCREATED_AT")
	for _, t := range list.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.PublicID, t.Title, t.Status, t.TaskType, t.ProjectID,
			t.CreatedAt.Format("2006-01-02T15:04:05Z"))
	}
	w.Flush()

	fmt.Fprintf(os.Stderr, "\nTotal: %d, Offset: %d, Limit: %d\n", list.Total, list.Offset, list.Limit)
}

func printTaskDetail(out *taskDetailOutput) {
	if taskJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	t := out.Task
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "PublicID:\t%s\n", t.PublicID)
	fmt.Fprintf(w, "Title:\t%s\n", t.Title)
	fmt.Fprintf(w, "Description:\t%s\n", t.Description)
	fmt.Fprintf(w, "Status:\t%s\n", t.Status)
	fmt.Fprintf(w, "TaskType:\t%s\n", t.TaskType)
	fmt.Fprintf(w, "OrgID:\t%d\n", t.OrgID)
	if out.OwnerName != "" {
		fmt.Fprintf(w, "Owner:\t%s (id=%d)\n", out.OwnerName, t.OwnerID)
	} else {
		fmt.Fprintf(w, "OwnerID:\t%d\n", t.OwnerID)
	}

	if out.Project != nil {
		fmt.Fprintf(w, "Project:\t%s (%s)\n", out.Project.Name, t.ProjectID)
	} else {
		fmt.Fprintf(w, "ProjectID:\t%s\n", t.ProjectID)
	}

	if out.Assignee != nil {
		fmt.Fprintf(w, "Assignee:\t%s (ID=%d, Code=%s)\n", out.Assignee.Name, out.Assignee.ID, out.Assignee.Code)
	} else if out.AssigneeName != "" {
		fmt.Fprintf(w, "Assignee:\t%s (id=%d)\n", out.AssigneeName, *t.AssigneeID)
	} else if t.AssigneeID != nil {
		fmt.Fprintf(w, "AssigneeID:\t%d\n", *t.AssigneeID)
	}

	if t.SessionID != nil {
		fmt.Fprintf(w, "SessionID:\t%d (internal)\n", *t.SessionID)
	}
	if t.Deadline != nil {
		fmt.Fprintf(w, "Deadline:\t%s\n", t.Deadline.Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(w, "CreatedAt:\t%s\n", t.CreatedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(w, "UpdatedAt:\t%s\n", t.UpdatedAt.Format("2006-01-02T15:04:05Z"))

	if len(out.Files) > 0 {
		fmt.Fprintf(w, "--- Files (%d) ---\t\n", len(out.Files))
		for i, f := range out.Files {
			size := formatTaskSize(f.Size)
			fmt.Fprintf(w, "  [%d] %s\t%s (%s)\n", i+1, f.Name, f.MimeType, size)
		}
	}

	w.Flush()
}

func formatTaskSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
