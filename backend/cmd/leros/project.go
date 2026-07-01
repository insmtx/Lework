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
	projectJSON    bool
	projectKeyword string
	projectStatus  string
	projectOffset  int
	projectLimit   int
)

func newProjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
		Long:  `Manage projects in the Leros platform.`,
	}

	lsCmd := &cobra.Command{
		Use:   "ls",
		Short: "List projects",
		Long:  `List all projects with optional filtering.`,
		Run: func(cmd *cobra.Command, args []string) {
			go func() {
				req := &contract.ListProjectsRequest{
					Pagination: contract.ListProjectsRequest{}.Pagination,
				}
				req.Offset = projectOffset
				req.Limit = projectLimit
				req.Fill()

				if projectKeyword != "" {
					req.Keyword = &projectKeyword
				}
				if projectStatus != "" {
					req.Status = &projectStatus
				}

				result, err := cli.ListProjects(lifecycle.Std().Context(), cliServerAddr(), cliAuthToken(), req)
				if err != nil {
					logs.Errorf("list projects: %v", err)
					lifecycle.Std().Exit()
					return
				}
				printProjects(result)
				lifecycle.Std().Exit()
			}()
			lifecycle.Std().WaitExit()
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <project_id>",
		Short: "Get project details",
		Long:  `Get detailed information about a specific project by its public ID.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			go func() {
				publicID := args[0]
				result, err := cli.DetailProject(lifecycle.Std().Context(), cliServerAddr(), cliAuthToken(), publicID)
				if err != nil {
					logs.Errorf("get project: %v", err)
					lifecycle.Std().Exit()
					return
				}
				projectFiles, pErr := cli.ListProjectFiles(lifecycle.Std().Context(), cliServerAddr(), cliAuthToken(), publicID, "artifact", "")
				if pErr != nil {
					logs.Warnf("list project files: %v", pErr)
				}
				printProjectDetail(result, projectFiles)
				lifecycle.Std().Exit()
			}()
			lifecycle.Std().WaitExit()
		},
	}

	cmd.PersistentFlags().BoolVar(&projectJSON, "json", false, "Output in JSON format")

	lsCmd.Flags().StringVar(&projectKeyword, "keyword", "", "Filter by name keyword")
	lsCmd.Flags().StringVar(&projectStatus, "status", "", "Filter by status")
	lsCmd.Flags().IntVar(&projectOffset, "offset", 0, "Pagination offset")
	lsCmd.Flags().IntVar(&projectLimit, "limit", 20, "Pagination limit")

	cmd.AddCommand(lsCmd)
	cmd.AddCommand(getCmd)
	return cmd
}

func printProjects(list *contract.ProjectList) {
	if projectJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(list.Items)
		return
	}

	if len(list.Items) == 0 {
		fmt.Println("No projects found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PUBLIC_ID\tNAME\tSTATUS\tCREATED_AT")
	for _, p := range list.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.PublicID, p.Name, p.Status, p.CreatedAt.Format("2006-01-02T15:04:05Z"))
	}
	w.Flush()

	fmt.Fprintf(os.Stderr, "\nTotal: %d, Offset: %d, Limit: %d\n", list.Total, list.Offset, list.Limit)
}

func printProjectDetail(d *contract.ProjectDetail, projectFiles []contract.FileTreeNode) {
	if projectJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(d)
		return
	}

	ownerName := resolveOwnerName(d.OwnerID, d.Members)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "PublicID:\t%s\n", d.PublicID)
	fmt.Fprintf(w, "Name:\t%s\n", d.Name)
	fmt.Fprintf(w, "Description:\t%s\n", d.Description)
	fmt.Fprintf(w, "Objective:\t%s\n", d.Objective)
	fmt.Fprintf(w, "Status:\t%s\n", d.Status)
	fmt.Fprintf(w, "Owner:\t%s\n", ownerName)
	fmt.Fprintf(w, "CreatedAt:\t%s\n", d.CreatedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(w, "UpdatedAt:\t%s\n", d.UpdatedAt.Format("2006-01-02T15:04:05Z"))

	if d.Session != nil {
		fmt.Fprintf(w, "--- Session ---\t\n")
		fmt.Fprintf(w, "  SessionID:\t%s\n", d.Session.SessionID)
		fmt.Fprintf(w, "  Type:\t%s\n", d.Session.Type)
		fmt.Fprintf(w, "  Status:\t%s\n", d.Session.Status)
		fmt.Fprintf(w, "  Title:\t%s\n", d.Session.Title)
		fmt.Fprintf(w, "  MessageCount:\t%d\n", d.Session.MessageCount)
	}

	if len(d.Tasks) > 0 {
		fmt.Fprintf(w, "--- Tasks (%d) ---\t\n", len(d.Tasks))
		for i, t := range d.Tasks {
			fmt.Fprintf(w, "  [%d] %s\t%s\n", i+1, t.Title, t.Status)
			if t.Session != nil {
				fmt.Fprintf(w, "    Session:\t%s (%s, %d msgs)\n", t.Session.SessionID, t.Session.Status, t.Session.MessageCount)
			}
		}
	}

	if len(projectFiles) > 0 {
		fmt.Fprintf(w, "--- Project Files (%d) ---\t\n", len(projectFiles))
		for i, f := range projectFiles {
			size := formatSize(f.Size)
			fmt.Fprintf(w, "  [%d] %s\t%s (%s)\n", i+1, f.Name, f.MimeType, size)
		}
	}

	if len(d.Members) > 0 {
		fmt.Fprintf(w, "--- Members (%d) ---\t\n", len(d.Members))
		for _, m := range d.Members {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", m.Name, m.MemberRole, m.MemberType)
		}
	}

	w.Flush()
}

func resolveOwnerName(ownerID uint, members []contract.ProjectMemberItem) string {
	for _, m := range members {
		if m.MemberID == ownerID {
			if m.Name != "" {
				return m.Name
			}
			return fmt.Sprintf("%d", ownerID)
		}
	}
	return fmt.Sprintf("%d", ownerID)
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
