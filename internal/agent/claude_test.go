package agent

import "testing"

func TestClaudePrepareCronArgsInjectsPrint(t *testing.T) {
	argv, sessionID, err := (claudeIntegration{}).PrepareCronArgs([]string{"review the backlog"})
	if err != nil {
		t.Fatalf("PrepareCronArgs: %v", err)
	}
	if sessionID == "" {
		t.Fatal("PrepareCronArgs returned empty sessionID")
	}
	if len(argv) != 4 {
		t.Fatalf("len(argv) = %d, want 4 (%v)", len(argv), argv)
	}
	if argv[0] != "--session-id" || argv[1] != sessionID {
		t.Fatalf("argv prefix = %q %q, want --session-id %q", argv[0], argv[1], sessionID)
	}
	if argv[2] != "--print" {
		t.Fatalf("argv[2] = %q, want --print", argv[2])
	}
	if argv[3] != "review the backlog" {
		t.Fatalf("argv[3] = %q, want prompt", argv[3])
	}
}

func TestClaudePrepareCronArgsKeepsExistingPrintFlag(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{name: "long flag", argv: []string{"--print", "review the backlog"}},
		{name: "short flag", argv: []string{"-p", "review the backlog"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv, sessionID, err := (claudeIntegration{}).PrepareCronArgs(tt.argv)
			if err != nil {
				t.Fatalf("PrepareCronArgs: %v", err)
			}
			if sessionID == "" {
				t.Fatal("PrepareCronArgs returned empty sessionID")
			}

			var printCount int
			for _, arg := range argv {
				if arg == "--print" || arg == "-p" {
					printCount++
				}
			}
			if printCount != 1 {
				t.Fatalf("print flag count = %d, want 1 (%v)", printCount, argv)
			}
		})
	}
}
