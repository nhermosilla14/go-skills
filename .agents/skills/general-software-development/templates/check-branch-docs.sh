#!/bin/sh
# Merge gate: fails while a branch still carries unfinished records.
# See reference/branches.md in the general-software-development skill.
fail=0

left=$(git ls-files docs/branch | grep -v '^docs/branch/\.gitkeep$')
if [ -n "$left" ]; then
  echo "Unfinalized files in docs/branch/:"
  echo "$left"
  fail=1
fi

for f in docs/spec/*.md; do
  [ -f "$f" ] || continue
  [ "$(basename "$f")" = "README.md" ] && continue
  bad=$(awk '
    function check() { if (title != "" && !ok) print "  " title }
    /^#/ { check(); title = ""; ok = 0 }
    /^### / { title = substr($0, 5) }
    /^- (Tests|Unverified):[ \t]*[^ \t]/ { ok = 1 }
    END { check() }' "$f")
  if [ -n "$bad" ]; then
    echo "Spec entries in $f with neither Tests nor Unverified:"
    echo "$bad"
    fail=1
  fi
done

for f in docs/adr/*.md; do
  [ -f "$f" ] || continue
  if grep -q '^- Status:[[:space:]]*Proposed' "$f"; then
    echo "ADR still Proposed: $f"
    fail=1
  fi
done

exit $fail
