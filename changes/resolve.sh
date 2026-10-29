#!/usr/bin/env bash
set -euo pipefail
if [[ "$EVENT" != pull_request ]]; then
  printf 'groups=[]\nall=true\n'
  exit 0
fi
jq -nr --argjson files "$FILES" --argjson filters "$FILTERS" '
  ($filters.changes | fromjson) as $groups |
  [$groups[] as $group | $filters[$group + "_files"] | fromjson | .[]] as $covered |
  "groups=\($groups | tojson)",
  "all=\(($groups | index("all") != null) or (($files - $covered) | length > 0))"
'
