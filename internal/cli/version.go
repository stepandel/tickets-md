package cli

// version is the binary version reported by `tickets --version`.
// Stamp it at build time with:
//
//	go build -ldflags "-X tickets-md/internal/cli.version=v0.1.0" ./cmd/tickets
//
// `make release VERSION=v0.1.0` does this automatically.
var version = "dev"
