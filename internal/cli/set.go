package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/ticket"
)

// setFields lists the scalar frontmatter fields that `tickets set`
// can mutate. Slice-valued fields (labels, related, blocked_by,
// blocks) are intentionally excluded — use `tickets link` or
// `tickets edit` for those.
var setFields = []string{"priority", "title"}

func newSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <id> <field> <value...>",
		Short: "Set a scalar field on a ticket (priority, title)",
		Long: `Set a scalar field on an existing ticket.

Supported fields: ` + strings.Join(setFields, ", ") + `.

Pass "-" as the value to clear a field. Multi-word values don't need
quoting — all arguments after the field name are joined with spaces.`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			id := args[0]
			field := strings.ToLower(args[1])
			value := strings.Join(args[2:], " ")
			if value == "-" {
				value = ""
			}
			t, err := s.Get(id)
			if err != nil {
				return err
			}
			if err := setField(&t, field, value); err != nil {
				return err
			}
			if err := s.Save(t); err != nil {
				return err
			}
			if value == "" {
				fmt.Printf("Cleared %s on %s\n", field, t.ID)
			} else {
				fmt.Printf("Set %s=%q on %s\n", field, value, t.ID)
			}
			return nil
		},
	}
	return cmd
}

func setField(t *ticket.Ticket, field, value string) error {
	switch field {
	case "priority":
		t.Priority = value
	case "title":
		if value == "" {
			return fmt.Errorf("title cannot be empty")
		}
		t.Title = value
	case "labels", "related", "blocked_by", "blocks":
		return fmt.Errorf("field %q is a list — use `tickets link`/`tickets edit` instead", field)
	default:
		return fmt.Errorf("unknown field %q (supported: %s)", field, strings.Join(setFields, ", "))
	}
	return nil
}
