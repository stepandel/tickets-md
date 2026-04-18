package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stepandel/tickets-md/internal/ticket"
)

type newRelations struct {
	parent    string
	blockedBy []string
	blocks    []string
	related   []string
}

func validateNewRelations(s *ticket.Store, parent string, blockedBy, blocks, related []string) (newRelations, error) {
	rels := newRelations{}
	seenByRole := map[string]map[string]struct{}{
		"parent":     {},
		"blocked_by": {},
		"blocks":     {},
		"related":    {},
	}
	flagByID := map[string]string{}

	add := func(flagName, role, id string) error {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("%s requires a non-empty ticket ID", flagName)
		}
		if _, ok := seenByRole[role][id]; ok {
			return fmt.Errorf("%s cannot contain duplicate ticket ID %q", flagName, id)
		}
		if priorFlag, ok := flagByID[id]; ok {
			return fmt.Errorf("ticket ID %q cannot be used with both %s and %s", id, priorFlag, flagName)
		}
		if _, err := s.Get(id); err != nil {
			return fmt.Errorf("%s %q: %w", flagName, id, err)
		}
		seenByRole[role][id] = struct{}{}
		flagByID[id] = flagName
		switch role {
		case "parent":
			rels.parent = id
		case "blocked_by":
			rels.blockedBy = append(rels.blockedBy, id)
		case "blocks":
			rels.blocks = append(rels.blocks, id)
		case "related":
			rels.related = append(rels.related, id)
		}
		return nil
	}

	if parent != "" {
		if err := add("--parent", "parent", parent); err != nil {
			return newRelations{}, err
		}
	}
	for _, id := range blockedBy {
		if err := add("--blocked-by", "blocked_by", id); err != nil {
			return newRelations{}, err
		}
	}
	for _, id := range blocks {
		if err := add("--blocks", "blocks", id); err != nil {
			return newRelations{}, err
		}
	}
	for _, id := range related {
		if err := add("--related", "related", id); err != nil {
			return newRelations{}, err
		}
	}

	return rels, nil
}

func newNewCmd() *cobra.Command {
	var priority string
	var project string
	var parent string
	var body string
	var labels []string
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
			rels, err := validateNewRelations(s, parent, blockedBy, blocks, related)
			if err != nil {
				return err
			}
			resolvedLabels, err := resolveConfiguredLabels(s.Config, labels)
			if err != nil {
				return err
			}
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
			if len(resolvedLabels) > 0 {
				t.Labels = append([]string(nil), resolvedLabels...)
				scalarChanged = true
			}
			if scalarChanged {
				if err := s.Save(t); err != nil {
					return err
				}
			}
			if rels.parent != "" {
				if err := s.Link(t.ID, rels.parent, "parent"); err != nil {
					return err
				}
			}
			for _, id := range rels.blockedBy {
				if err := s.Link(t.ID, id, "blocked_by"); err != nil {
					return err
				}
			}
			for _, id := range rels.blocks {
				if err := s.Link(id, t.ID, "blocked_by"); err != nil {
					return err
				}
			}
			for _, id := range rels.related {
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
			if len(resolvedLabels) > 0 {
				extras = append(extras, "labels: "+renderLabels(resolvedLabels))
			}
			if rels.parent != "" {
				extras = append(extras, "parent: "+rels.parent)
			}
			if len(rels.blockedBy) > 0 {
				extras = append(extras, "blocked_by: "+strings.Join(rels.blockedBy, ", "))
			}
			if len(rels.blocks) > 0 {
				extras = append(extras, "blocks: "+strings.Join(rels.blocks, ", "))
			}
			if len(rels.related) > 0 {
				extras = append(extras, "related: "+strings.Join(rels.related, ", "))
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
	cmd.Flags().StringSliceVar(&labels, "label", nil, "set one or more configured labels")
	cmd.Flags().StringVar(&project, "project", "", "set the ticket's project ID")
	cmd.Flags().StringVar(&parent, "parent", "", "set the new ticket's parent ticket ID")
	cmd.Flags().StringSliceVar(&blockedBy, "blocked-by", nil, "set one or more blocking ticket IDs")
	cmd.Flags().StringSliceVar(&blocks, "blocks", nil, "set one or more ticket IDs blocked by the new ticket")
	cmd.Flags().StringSliceVar(&related, "related", nil, "set one or more related ticket IDs")
	return cmd
}

func normalizeBodyFlag(body string) string {
	var b strings.Builder
	b.Grow(len(body))

	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			b.WriteByte(body[i])
			continue
		}
		if i+1 >= len(body) {
			b.WriteByte('\\')
			continue
		}

		switch body[i+1] {
		case 'n':
			b.WriteByte('\n')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case '\\':
			b.WriteByte('\\')
			i++
		default:
			b.WriteByte('\\')
		}
	}

	return b.String()
}
