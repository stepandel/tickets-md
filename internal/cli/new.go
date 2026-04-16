package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var priority string
	var project string
	var parent string
	var body string
	var blockedBy []string
	var blocks []string
	var related []string
	cmd := &cobra.Command{
		Use:   "new <title...>",
		Short: "Create a new ticket in the default stage",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			title := strings.Join(args, " ")
			t, err := s.Create(title)
			if err != nil {
				return err
			}
			scalarChanged := false
			if body != "" {
				t.Body = normalizeBodyFlag(body)
				scalarChanged = true
			}
			if priority != "" {
				t.Priority = priority
				scalarChanged = true
			}
			if project != "" {
				if _, err := s.GetProject(project); err != nil {
					return err
				}
				t.Project = project
				scalarChanged = true
			}
			if scalarChanged {
				if err := s.Save(t); err != nil {
					return err
				}
			}
			if parent != "" {
				if err := s.Link(t.ID, parent, "parent"); err != nil {
					return err
				}
			}
			for _, id := range blockedBy {
				if err := s.Link(t.ID, id, "blocked_by"); err != nil {
					return err
				}
			}
			for _, id := range blocks {
				if err := s.Link(id, t.ID, "blocked_by"); err != nil {
					return err
				}
			}
			for _, id := range related {
				if err := s.Link(t.ID, id, "related"); err != nil {
					return err
				}
			}
			var extras []string
			if priority != "" {
				extras = append(extras, "priority: "+priority)
			}
			if project != "" {
				extras = append(extras, "project: "+project)
			}
			if parent != "" {
				extras = append(extras, "parent: "+parent)
			}
			if len(blockedBy) > 0 {
				extras = append(extras, "blocked_by: "+strings.Join(blockedBy, ", "))
			}
			if len(blocks) > 0 {
				extras = append(extras, "blocks: "+strings.Join(blocks, ", "))
			}
			if len(related) > 0 {
				extras = append(extras, "related: "+strings.Join(related, ", "))
			}
			if len(extras) > 0 {
				fmt.Printf("Created %s in %s (%s)\n  %s\n", t.ID, t.Stage, strings.Join(extras, ", "), t.Path)
			} else {
				fmt.Printf("Created %s in %s\n  %s\n", t.ID, t.Stage, t.Path)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&body, "body", "b", "", "set the ticket body markdown")
	cmd.Flags().StringVarP(&priority, "priority", "p", "", "set ticket priority (e.g. low, medium, high, critical)")
	cmd.Flags().StringVar(&project, "project", "", "set the ticket's project ID")
	cmd.Flags().StringVar(&parent, "parent", "", "set the new ticket's parent ticket ID")
	cmd.Flags().StringSliceVar(&blockedBy, "blocked-by", nil, "set one or more blocking ticket IDs")
	cmd.Flags().StringSliceVar(&blocks, "blocks", nil, "set one or more ticket IDs blocked by the new ticket")
	cmd.Flags().StringSliceVar(&related, "related", nil, "set one or more related ticket IDs")
	return cmd
}

func normalizeBodyFlag(body string) string {
	return strings.ReplaceAll(body, `\n`, "\n")
}
