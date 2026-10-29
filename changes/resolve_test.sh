#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
check() {
  local name="$1" expected="$2" actual
  actual=$(EVENT="$3" FILES="$4" FILTERS="$5" bash resolve.sh)
  if [[ "$actual" != "$expected" ]]; then
    printf '%s: expected %s, got %s\n' "$name" "$expected" "$actual" >&2
    exit 1
  fi
}
check matched $'groups=["go"]\nall=false' pull_request '["main.go"]' \
  '{"changes":"[\"go\"]","go_files":"[\"main.go\"]"}'
check unknown $'groups=["go"]\nall=true' pull_request '["main.go","Dockerfile"]' \
  '{"changes":"[\"go\"]","go_files":"[\"main.go\"]"}'
check all $'groups=["all"]\nall=true' pull_request '["Makefile"]' \
  '{"changes":"[\"all\"]","all_files":"[\"Makefile\"]"}'
check empty $'groups=[]\nall=false' pull_request '[]' '{"changes":"[]"}'
check push $'groups=[]\nall=true' push '' '{}'
check overlap $'groups=["go","desktop"]\nall=false' pull_request '["icon.png"]' \
  '{"changes":"[\"go\",\"desktop\"]","go_files":"[\"icon.png\"]","desktop_files":"[\"icon.png\"]"}'
check filenames $'groups=["docs"]\nall=false' pull_request '["docs/a b.md","docs/a\nb.md"]' \
  '{"changes":"[\"docs\"]","docs_files":"[\"docs/a b.md\",\"docs/a\\nb.md\"]"}'
printf 'Resolver cases checked.\n'
