.PHONY: install build check test vet

install: build
	@echo "done — run 'tickets --help' to get started"

build:
	go install ./cmd/tickets

check: build vet test

vet:
	go vet ./...

test:
	go test ./...
