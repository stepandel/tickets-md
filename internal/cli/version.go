package cli

import "runtime/debug"

// version is what `tickets --version` reports. Resolution order:
//
//  1. -ldflags "-X …/internal/cli.version=…" (used by `make release`).
//  2. The module version embedded by `go install …@vX.Y.Z`, read from
//     runtime/debug.ReadBuildInfo.
//  3. "dev" for plain `go build` / `go install ./cmd/tickets` from a
//     local checkout.
var version = resolveVersion()

// linkerVersion is what the release Makefile stamps via -ldflags. We
// keep it as a separate variable so the ldflag target does not change
// and so resolveVersion can distinguish a missing stamp (empty string)
// from a real one.
var linkerVersion = ""

func resolveVersion() string {
	if linkerVersion != "" {
		return linkerVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
