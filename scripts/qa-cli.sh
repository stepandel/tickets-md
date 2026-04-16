#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
bin_dir="$tmp_dir/bin"
project_dir="$tmp_dir/project"
tickets_bin="$bin_dir/tickets"
watch_log="$tmp_dir/watch.log"
doctor_log="$tmp_dir/doctor.log"
watch_pid=""

timeout_tenths="${QA_TIMEOUT_TENTHS:-300}"
ready_marker='ready — move tickets between stages to trigger agents'

cleanup() {
	if [[ -n "$watch_pid" ]] && kill -0 "$watch_pid" 2>/dev/null; then
		kill "$watch_pid" 2>/dev/null || true
		wait "$watch_pid" 2>/dev/null || true
	fi
	rm -rf "$tmp_dir"
}
trap cleanup EXIT

mkdir -p "$bin_dir" "$project_dir"

echo "==> building tickets"
(
	cd "$repo_root"
	go build -o "$tickets_bin" ./cmd/tickets
)

echo "==> creating temp git repo"
git -C "$project_dir" init --quiet
git -C "$project_dir" config user.name "QA Harness"
git -C "$project_dir" config user.email "qa-harness@example.com"
cat >"$project_dir/README.md" <<'EOF'
# QA harness fixture
EOF
git -C "$project_dir" add README.md
git -C "$project_dir" commit --quiet -m "Initial commit"

echo "==> initializing ticket store"
"$tickets_bin" -C "$project_dir" init --stages backlog,execute,done
cat >"$project_dir/.tickets/execute/.stage.yml" <<'EOF'
agent:
  command: sh
  args: ["-lc", "exit 0"]
  prompt: |
    qa-cli harness
EOF

echo "==> exercising CLI flow"
"$tickets_bin" -C "$project_dir" new "QA harness smoke test"
"$tickets_bin" -C "$project_dir" list >/dev/null
"$tickets_bin" -C "$project_dir" show TIC-001 >/dev/null

echo "==> starting watcher"
"$tickets_bin" -C "$project_dir" watch >"$watch_log" 2>&1 &
watch_pid=$!

for _ in $(seq 1 "$timeout_tenths"); do
	if grep -qF "$ready_marker" "$watch_log"; then
		break
	fi
	sleep 0.1
done

if ! grep -qF "$ready_marker" "$watch_log"; then
	echo "qa-cli: watcher did not report readiness" >&2
	cat "$watch_log" >&2 || true
	exit 1
fi

echo "==> moving ticket into execute"
"$tickets_bin" -C "$project_dir" move TIC-001 execute

run_file="$project_dir/.tickets/.agents/TIC-001/001-execute.yml"
ticket_file="$project_dir/.tickets/execute/TIC-001.md"

for _ in $(seq 1 "$timeout_tenths"); do
	if [[ -f "$run_file" ]] && grep -qE '^status: done$' "$run_file"; then
		break
	fi
	sleep 0.1
done

if [[ ! -f "$run_file" ]]; then
	echo "qa-cli: expected run YAML at $run_file" >&2
	cat "$watch_log" >&2 || true
	exit 1
fi

if ! grep -qE '^status: done$' "$run_file"; then
	echo "qa-cli: run did not reach done status" >&2
	cat "$run_file" >&2
	cat "$watch_log" >&2 || true
	exit 1
fi

if ! grep -qE '^agent_status: done$' "$ticket_file"; then
	echo "qa-cli: ticket frontmatter was not synced to done" >&2
	cat "$ticket_file" >&2
	exit 1
fi

echo "==> stopping watcher"
kill "$watch_pid"
wait "$watch_pid" || true
watch_pid=""

echo "==> verifying doctor is clean"
"$tickets_bin" -C "$project_dir" doctor --dry-run >"$doctor_log"
if ! grep -qE '^Nothing to do\.$' "$doctor_log"; then
	echo "qa-cli: doctor reported drift" >&2
	cat "$doctor_log" >&2
	exit 1
fi

echo "qa-cli: ok"
