#!/bin/sh
# Fails if the production web bundle is over its gzip budget. The budgets live
# here and nowhere else; 1 kB is 1000 bytes, as Vite reports sizes.
#
#   web-budget.sh [DIST]     DIST defaults to apps/web/dist
set -u

js_budget_kb=150
css_budget_kb=20

assets=${1:-apps/web/dist}/assets
[ -d "$assets" ] || {
  printf 'web-budget: no build in %s; run make web-build\n' "$assets" >&2
  exit 1
}

over=0

# check EXT LABEL BUDGET_KB: prints the gzip size of every EXT file and fails
# the run if their total is over BUDGET_KB.
check() {
  total=0
  for file in "$assets"/*."$1"; do
    [ -e "$file" ] || continue
    size=$(gzip -c "$file" | wc -c | tr -d ' ')
    total=$((total + size))
    printf '  %-36s %9s B\n' "${file##*/}" "$size"
  done
  budget=$(($3 * 1000))
  if [ "$total" -le "$budget" ]; then
    printf '  %-36s %9s B of %s B, ok\n' "$2 total" "$total" "$budget"
  else
    printf '  %-36s %9s B of %s B, OVER BUDGET\n' "$2 total" "$total" "$budget"
    over=1
  fi
}

echo 'web-budget: gzip sizes'
check js JS "$js_budget_kb"
check css CSS "$css_budget_kb"
[ "$over" -eq 0 ] || {
  echo 'web-budget: the bundle is over budget' >&2
  exit 1
}
