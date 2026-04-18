package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

const defaultNewLabelColor = "#6b7280"

func newLabelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label <id> <label> [<label>...]",
		Short: "Add configured labels to a ticket",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Get(args[0])
			if err != nil {
				return err
			}
			labels, err := resolveConfiguredLabels(s.Config, args[1:])
			if err != nil {
				return err
			}
			added := addTicketLabels(&t, labels)
			if err := s.Save(t); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Labeled %s: %s\n", t.ID, renderLabelsOrNone(added))
			return nil
		},
	}
	return cmd
}

func newUnlabelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlabel <id> <label> [<label>...]",
		Short: "Remove labels from a ticket",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Get(args[0])
			if err != nil {
				return err
			}
			names, err := normalizeUniqueLabels(args[1:], "label arguments")
			if err != nil {
				return err
			}
			removed := removeTicketLabels(&t, names)
			if err := s.Save(t); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unlabeled %s: %s\n", t.ID, renderLabelsOrNone(removed))
			return nil
		},
	}
	return cmd
}

func newLabelsCmd() *cobra.Command {
	var onTicket string
	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List configured labels or labels on a ticket",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if onTicket != "" {
				t, err := s.Get(onTicket)
				if err != nil {
					return err
				}
				if len(t.Labels) == 0 {
					fmt.Fprintln(out, "(none)")
					return nil
				}
				for _, label := range t.Labels {
					fmt.Fprintln(out, label)
				}
				return nil
			}
			for _, label := range s.Config.OrderedLabelNames() {
				fmt.Fprintln(out, label)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&onTicket, "on", "", "show the labels currently assigned to a ticket")
	cmd.AddCommand(newLabelsCreateCmd())
	return cmd
}

func newLabelsCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a configured label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			name := strings.TrimSpace(args[0])
			normalized := normalizeLabelName(name)
			if normalized == "" {
				return fmt.Errorf("label name is required")
			}
			if normalized == "none" {
				return fmt.Errorf(`label "none" is reserved`)
			}
			if key, ok := canonicalConfiguredLabel(s.Config, name); ok {
				return fmt.Errorf("label %q already exists as %q", name, key)
			}
			if s.Config.Labels == nil {
				s.Config.Labels = map[string]config.LabelConfig{}
			}
			s.Config.Labels[name] = config.LabelConfig{Color: defaultNewLabelColor}
			if err := config.Save(s.Root, s.Config); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created label %q (color %s)\n", name, defaultNewLabelColor)
			return nil
		},
	}
}

func normalizeLabelName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func canonicalConfiguredLabel(cfg config.Config, name string) (string, bool) {
	normalized := normalizeLabelName(name)
	if normalized == "" || cfg.Labels == nil {
		return "", false
	}
	for key := range cfg.Labels {
		if normalizeLabelName(key) == normalized {
			return key, true
		}
	}
	return "", false
}

func normalizeUniqueLabels(values []string, source string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeLabelName(value)
		if normalized == "" {
			return nil, fmt.Errorf("%s cannot contain an empty label", source)
		}
		if normalized == "none" {
			return nil, fmt.Errorf("label %q is reserved", strings.TrimSpace(value))
		}
		if _, dup := seen[normalized]; dup {
			return nil, fmt.Errorf("%s cannot contain duplicate label %q", source, strings.TrimSpace(value))
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func resolveConfiguredLabels(cfg config.Config, values []string) ([]string, error) {
	normalized, err := normalizeUniqueLabels(values, "label arguments")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(normalized))
	for _, name := range normalized {
		key, ok := canonicalConfiguredLabel(cfg, name)
		if !ok {
			return nil, fmt.Errorf("unknown label %q", name)
		}
		out = append(out, key)
	}
	return out, nil
}

func ticketHasLabel(t ticket.Ticket, name string) bool {
	normalized := normalizeLabelName(name)
	for _, existing := range t.Labels {
		if normalizeLabelName(existing) == normalized {
			return true
		}
	}
	return false
}

func addTicketLabels(t *ticket.Ticket, labels []string) []string {
	added := make([]string, 0, len(labels))
	for _, label := range labels {
		if ticketHasLabel(*t, label) {
			continue
		}
		t.Labels = append(t.Labels, label)
		added = append(added, label)
	}
	return added
}

func removeTicketLabels(t *ticket.Ticket, normalized []string) []string {
	if len(normalized) == 0 || len(t.Labels) == 0 {
		return nil
	}
	removeSet := make(map[string]struct{}, len(normalized))
	for _, name := range normalized {
		removeSet[name] = struct{}{}
	}
	var kept []string
	var removed []string
	for _, label := range t.Labels {
		if _, ok := removeSet[normalizeLabelName(label)]; ok {
			removed = append(removed, label)
			continue
		}
		kept = append(kept, label)
	}
	t.Labels = kept
	return removed
}

func renderLabels(labels []string) string {
	return strings.Join(labels, ", ")
}

func renderLabelsOrNone(labels []string) string {
	if len(labels) == 0 {
		return "(none)"
	}
	return renderLabels(labels)
}

func configuredAndUnconfiguredLabels(cfg config.Config, assigned []string) []pickerItem {
	items := make([]pickerItem, 0, len(cfg.Labels)+len(assigned)+1)
	for _, label := range cfg.OrderedLabelNames() {
		marker := ""
		if slices.ContainsFunc(assigned, func(existing string) bool {
			return normalizeLabelName(existing) == normalizeLabelName(label)
		}) {
			marker = "(assigned)"
		}
		items = append(items, pickerItem{label: label, key: marker, value: label})
	}
	for _, label := range assigned {
		if _, ok := canonicalConfiguredLabel(cfg, label); ok {
			continue
		}
		items = append(items, pickerItem{label: label, key: "(unconfigured)", value: label})
	}
	items = append(items, pickerItem{label: "+ create label...", value: createLabelSentinel{}})
	return items
}

type createLabelSentinel struct{}
