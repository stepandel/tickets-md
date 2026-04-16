#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
bin_dir="$tmp_dir/bin"
tickets_bin="$bin_dir/tickets"

cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT

detect_obsidian_bin() {
	if [[ -n "${OBSIDIAN_BIN:-}" ]]; then
		printf '%s\n' "$OBSIDIAN_BIN"
		return 0
	fi

	case "$(uname -s)" in
		Darwin)
			if [[ -x "/Applications/Obsidian.app/Contents/MacOS/Obsidian" ]]; then
				printf '%s\n' "/Applications/Obsidian.app/Contents/MacOS/Obsidian"
				return 0
			fi
			;;
		Linux)
			for candidate in /usr/bin/obsidian /usr/local/bin/obsidian /snap/bin/obsidian; do
				if [[ -x "$candidate" ]]; then
					printf '%s\n' "$candidate"
					return 0
				fi
			done
			;;
	esac

	return 1
}

check_obsidian_cli() {
	local output
	if ! output="$("$1" --help 2>&1)"; then
		printf '%s\n' "$output"
		return 1
	fi
	printf '%s\n' "$output"
	if [[ "$output" == *"Command line interface is not enabled"* ]]; then
		return 2
	fi
	if [[ "$output" == *"installer is out of date"* ]]; then
		return 3
	fi
	return 0
}

if ! obsidian_bin="$(detect_obsidian_bin)"; then
	cat >&2 <<'EOF'
qa-plugin: Obsidian desktop binary not found.
Set OBSIDIAN_BIN to the Obsidian executable path, then rerun `make qa-plugin`.
EOF
	exit 2
fi

set +e
obsidian_check_output="$(check_obsidian_cli "$obsidian_bin")"
obsidian_check_status=$?
set -e

case $obsidian_check_status in
	0) ;;
	2)
		cat >&2 <<EOF
qa-plugin: Obsidian was found at $obsidian_bin, but its command line interface is disabled.
Enable Settings -> General -> Advanced -> Command line interface, then rerun \`make qa-plugin\`.

$obsidian_check_output
EOF
		exit 2
		;;
	3)
		cat >&2 <<EOF
qa-plugin: Obsidian was found at $obsidian_bin, but the installer reports outdated CLI support.
Update Obsidian to a build with CLI support, then rerun \`make qa-plugin\`.

$obsidian_check_output
EOF
		exit 2
		;;
	*)
		cat >&2 <<EOF
qa-plugin: failed to probe Obsidian at $obsidian_bin before running the smoke test.

$obsidian_check_output
EOF
		exit 2
		;;
esac

mkdir -p "$bin_dir"

echo "==> building tickets"
(
	cd "$repo_root"
	go build -o "$tickets_bin" ./cmd/tickets
)

echo "==> bundling plugin"
(
	cd "$repo_root/obsidian-plugin"
	npm ci --silent
	npm run build --silent
)

echo "==> running Obsidian smoke test"
(
	cd "$repo_root/obsidian-plugin"
	OBSIDIAN_BIN="$obsidian_bin" TICKETS_BIN="$tickets_bin" npm run test:e2e
)
