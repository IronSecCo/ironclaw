#!/usr/bin/env bash
#
# Reject binary build artifacts accidentally committed to the repo.
#
# Background: a 17MB compiled `controlplane` binary was once committed to the
# tree (see IRO-8). Build outputs have no business in git history — they bloat
# every clone. This guard fails CI if any tracked file is a binary blob that is
# not on the small allowlist of legitimately-binary assets (images, fonts, etc).
#
# Binary detection uses git's own numstat: for binary blobs git prints "-\t-"
# instead of added/removed line counts. We diff the empty tree against HEAD to
# enumerate every tracked file's blob.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

empty_tree="$(git hash-object -t tree /dev/null)"

# Extensions that are legitimately binary and allowed in the tree.
allow_re='\.(png|jpe?g|gif|ico|svg|woff2?|ttf|otf|eot|pdf)$'

violations=()
while IFS= read -r path; do
  [[ -z "$path" ]] && continue
  [[ "$path" =~ $allow_re ]] && continue
  violations+=("$path")
done < <(git diff --numstat "$empty_tree" HEAD | awk -F'\t' '$1=="-" && $2=="-" {print $3}')

if (( ${#violations[@]} > 0 )); then
  echo "ERROR: binary artifact(s) tracked in the repo:" >&2
  printf '  %s\n' "${violations[@]}" >&2
  echo >&2
  echo "Build outputs must not be committed. Fix with:" >&2
  echo "  git rm --cached <file>   # drop from the index" >&2
  echo "  echo '/<file>' >> .gitignore" >&2
  exit 1
fi

echo "OK: no binary build artifacts tracked."
