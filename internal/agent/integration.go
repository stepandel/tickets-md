package agent

// Integration is an optional per-agent extension point. Agents that
// produce a discoverable plan file (via a deterministic session id
// and transcript path) can register one so the watcher can link the
// plan back to the run. Agents without an integration still work —
// they just don't get plan extraction.
type Integration interface {
	// Name is the agent command this integration matches. It must
	// equal the command string configured in .stage.yml (e.g. "claude").
	Name() string

	// PrepareArgs runs before the agent is spawned. It may return a
	// modified argv (e.g. with a session flag injected) and an opaque
	// session id to persist in the run YAML. An empty session id
	// disables plan lookup for this run.
	PrepareArgs(argv []string) (newArgv []string, sessionID string, err error)

	// ExtractPlan returns the absolute path of a plan file produced
	// during the run. sessionID is whatever PrepareArgs returned; cwd
	// is the directory the agent was spawned in. ("", nil) means no
	// plan was found.
	ExtractPlan(sessionID, cwd string) (string, error)
}

var integrations = map[string]Integration{}

// Register adds an Integration to the package-level registry. Call
// from an init() in the file that defines the integration.
func Register(i Integration) {
	integrations[i.Name()] = i
}

// Lookup returns the Integration for command, if one is registered.
func Lookup(command string) (Integration, bool) {
	i, ok := integrations[command]
	return i, ok
}
