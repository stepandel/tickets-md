.PHONY: install build

install: build
	@echo "done — run 'tickets --help' to get started"

build:
	go install ./cmd/tickets
