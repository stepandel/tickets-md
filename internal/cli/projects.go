package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/ticket"
)

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage projects",
	}
	cmd.AddCommand(
		newProjectsNewCmd(),
		newProjectsListCmd(),
		newProjectsShowCmd(),
		newProjectsRmCmd(),
		newProjectsSetCmd(),
		newProjectsAssignCmd(),
		newProjectsUnassignCmd(),
	)
	return cmd
}

func newProjectsNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <title...>",
		Short: "Create a new project",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			p, err := s.CreateProject(strings.Join(args, " "))
			if err != nil {
				return err
			}
			fmt.Printf("Created %s\n  %s\n", p.ID, p.Path)
			return nil
		},
	}
}

func newProjectsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			projects, err := s.ListProjects()
			if err != nil {
				return err
			}
			fmt.Printf("[projects] (%d)\n", len(projects))
			if len(projects) == 0 {
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, p := range projects {
				status := p.Status
				if status == "" {
					status = "-"
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", p.ID, status, p.Title)
			}
			tw.Flush()
			return nil
		},
	}
	return cmd
}

func newProjectsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print a project's contents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(args[0])
			if err != nil {
				return err
			}
			data, err := os.ReadFile(p.Path)
			if err != nil {
				return err
			}
			fmt.Printf("# %s — %s\n", p.ID, p.Title)
			if p.Status != "" {
				fmt.Printf("# Status: %s\n", p.Status)
			}
			fmt.Printf("# %s\n\n", p.Path)
			_, err = os.Stdout.Write(data)
			return err
		},
	}
}

func newProjectsRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(args[0])
			if err != nil {
				return err
			}
			members, err := ticketsByProject(s, p.ID)
			if err != nil {
				return err
			}
			if !force {
				fmt.Printf("Delete %s (%s)? %d ticket(s) will be unassigned. [y/N] ", p.ID, p.Title, len(members))
				r := bufio.NewReader(os.Stdin)
				line, _ := r.ReadString('\n')
				if !strings.EqualFold(strings.TrimSpace(line), "y") {
					fmt.Println("aborted")
					return nil
				}
			}
			if err := s.DeleteProject(p.ID); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", p.ID)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}

func newProjectsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <id> <field> <value...>",
		Short: "Set a scalar field on a project (status, title)",
		Long: `Set a scalar field on an existing project.

Supported fields: status, title.

Pass "-" as the value to clear a field.`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(args[0])
			if err != nil {
				return err
			}
			field := strings.ToLower(args[1])
			value := strings.Join(args[2:], " ")
			if value == "-" {
				value = ""
			}
			switch field {
			case "status":
				p.Status = value
			case "title":
				if value == "" {
					return fmt.Errorf("title cannot be empty")
				}
				p.Title = value
			default:
				return fmt.Errorf("unknown field %q (supported: status, title)", field)
			}
			if err := s.SaveProject(p); err != nil {
				return err
			}
			if value == "" {
				fmt.Printf("Cleared %s on %s\n", field, p.ID)
			} else {
				fmt.Printf("Set %s=%q on %s\n", field, value, p.ID)
			}
			return nil
		},
	}
}

func newProjectsAssignCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "assign <ticket-id> <project-id>",
		Short: "Assign a ticket to a project",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Get(args[0])
			if err != nil {
				return err
			}
			p, err := s.GetProject(args[1])
			if err != nil {
				return err
			}
			t.Project = p.ID
			if err := s.Save(t); err != nil {
				return err
			}
			fmt.Printf("Assigned %s to %s\n", t.ID, p.ID)
			return nil
		},
	}
}

func newProjectsUnassignCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unassign <ticket-id>",
		Short: "Remove a ticket's project assignment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Get(args[0])
			if err != nil {
				return err
			}
			t.Project = ""
			if err := s.Save(t); err != nil {
				return err
			}
			fmt.Printf("Unassigned %s\n", t.ID)
			return nil
		},
	}
}

func ticketsByProject(s *ticket.Store, projectID string) ([]ticket.Ticket, error) {
	grouped, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	var ticketsForProject []ticket.Ticket
	for _, stage := range s.Config.Stages {
		for _, t := range grouped[stage] {
			if t.Project == projectID {
				ticketsForProject = append(ticketsForProject, t)
			}
		}
	}
	return ticketsForProject, nil
}
