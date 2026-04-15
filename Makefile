.PHONY: install build check test vet release plugin-bundle plugin-zip

PLUGIN_SRC := obsidian-plugin
PLUGIN_ZIP := dist/tickets-board-plugin.zip

install: build
	@echo "done — run 'tickets --help' to get started"

build:
	go install ./cmd/tickets

# plugin-bundle compiles the Obsidian plugin with esbuild. Outputs
# $(PLUGIN_SRC)/main.js alongside the hand-maintained manifest.json
# and styles.css. No longer a dependency of `make install`: the CLI
# downloads the matching release at install time instead of embedding
# the bundle. Developers who want to test `tickets obsidian install
# --from $(PLUGIN_SRC)` run this first.
plugin-bundle:
	cd $(PLUGIN_SRC) && npm ci --silent && npm run build --silent

# plugin-zip bundles the three plugin artefacts into the archive
# GoReleaser uploads as a release asset. `tickets obsidian install`
# pulls this zip from github.com/stepandel/tickets-md/releases.
plugin-zip: plugin-bundle
	@mkdir -p dist
	@rm -f $(PLUGIN_ZIP)
	cd $(PLUGIN_SRC) && zip -q -j ../$(PLUGIN_ZIP) main.js manifest.json styles.css
	@echo "wrote $(PLUGIN_ZIP)"

# release stamps the binary with VERSION via -ldflags so `tickets
# --version` reports the tag. Usage: `make release VERSION=v0.1.0`.
release:
	@if [ -z "$(VERSION)" ]; then \
		echo "usage: make release VERSION=v0.1.0" >&2; \
		exit 1; \
	fi
	go install -ldflags "-X github.com/stepandel/tickets-md/internal/cli.linkerVersion=$(VERSION)" ./cmd/tickets
	@echo "installed tickets $(VERSION)"

check: build vet test

vet:
	go vet ./...

test:
	go test ./...
