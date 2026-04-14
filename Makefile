.PHONY: install build check test vet release

install: build
	@echo "done — run 'tickets --help' to get started"

build:
	go install ./cmd/tickets

# release stamps the binary with VERSION via -ldflags so `tickets
# --version` reports the tag. Usage: `make release VERSION=v0.1.0`.
release:
	@if [ -z "$(VERSION)" ]; then \
		echo "usage: make release VERSION=v0.1.0" >&2; \
		exit 1; \
	fi
	go install -ldflags "-X tickets-md/internal/cli.version=$(VERSION)" ./cmd/tickets
	@echo "installed tickets $(VERSION)"

check: build vet test

vet:
	go vet ./...

test:
	go test ./...
