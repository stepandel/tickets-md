.PHONY: install build deps

install: deps build
	@echo "done — run 'tickets --help' to get started"

build:
	go install ./cmd/tickets

deps:
	@command -v tmux >/dev/null 2>&1 || { echo "Installing tmux..."; brew install tmux; }
