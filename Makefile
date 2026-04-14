.PHONY: install build check test vet release plugin-bundle

PLUGIN_ASSETS := internal/obsidian/assets
PLUGIN_SRC    := obsidian-plugin

install: plugin-bundle build
	@echo "done — run 'tickets --help' to get started"

build:
	go install ./cmd/tickets

# plugin-bundle rebuilds the Obsidian plugin and copies the artefacts
# that `internal/plugin` embeds. Must run before `go build` for the
# CLI to ship a real `tickets plugin install`.
plugin-bundle:
	cd $(PLUGIN_SRC) && npm ci --silent && npm run build --silent
	@mkdir -p $(PLUGIN_ASSETS)
	cp $(PLUGIN_SRC)/main.js       $(PLUGIN_ASSETS)/main.js
	cp $(PLUGIN_SRC)/manifest.json $(PLUGIN_ASSETS)/manifest.json
	cp $(PLUGIN_SRC)/styles.css    $(PLUGIN_ASSETS)/styles.css

# release stamps the binary with VERSION via -ldflags so `tickets
# --version` reports the tag. Usage: `make release VERSION=v0.1.0`.
release: plugin-bundle
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
