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
	taskServerAddr string
	taskJSON       bool
	taskKeyword    string
	taskStatus     string
	taskProjectID  uint
	taskType       string
	taskAssigneeID uint
	taskOffset     int
	taskLimit      int
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage tasks",
	Long:  `Manage tasks in the Leros platform.`,
}

var taskLsCmd = &cobra.Command{
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

			result, err := cli.ListTasks(lifecycle.Std().Context(), taskServerAddr, req)
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
		projectID := fmt.Sprintf("%d", t.ProjectID)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.PublicID, t.Title, t.Status, t.TaskType, projectID,
			t.CreatedAt.Format("2006-01-02T15:04:05Z"))
	}
	w.Flush()

	fmt.Fprintf(os.Stderr, "\nTotal: %d, Offset: %d, Limit: %d\n", list.Total, list.Offset, list.Limit)
}

func init() {
	taskLsCmd.Flags().StringVar(&taskServerAddr, "server-addr", "127.0.0.1:8080", "Leros server address (host:port)")
	taskLsCmd.Flags().BoolVar(&taskJSON, "json", false, "Output in JSON format")
	taskLsCmd.Flags().StringVar(&taskKeyword, "keyword", "", "Filter by title/description keyword")
	taskLsCmd.Flags().StringVar(&taskStatus, "status", "", "Filter by status")
	taskLsCmd.Flags().UintVar(&taskProjectID, "project-id", 0, "Filter by project ID")
	taskLsCmd.Flags().StringVar(&taskType, "type", "", "Filter by task type")
	taskLsCmd.Flags().UintVar(&taskAssigneeID, "assignee-id", 0, "Filter by assignee ID")
	taskLsCmd.Flags().IntVar(&taskOffset, "offset", 0, "Pagination offset")
	taskLsCmd.Flags().IntVar(&taskLimit, "limit", 20, "Pagination limit")

	taskCmd.AddCommand(taskLsCmd)
	rootCmd.AddCommand(taskCmd)
}
