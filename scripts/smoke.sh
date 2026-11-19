#!/bin/sh
# Smoke test for a built binary: version metadata, help text and one local install.
# Usage: scripts/smoke.sh [path/to/skills]
set -eu

root=$(CDPATH= cd "$(dirname "$0")/.." && pwd)

fail() {
  echo "smoke: $*" >&2
  exit 1
}

if [ "$#" -gt 0 ]; then
  bin=$1
else
  bin=
  if [ -x "$root/skills" ]; then
    bin=$root/skills
  elif [ -x "$root/skills.exe" ]; then
    bin=$root/skills.exe
  elif [ -d "$root/dist" ]; then
    bin=$(find "$root/dist" -type f \( -name skills -o -name skills.exe \) | sort | head -n 1)
  fi
fi

[ -n "$bin" ] || fail "no binary given and none found in . or dist/; run 'task build' first"
[ -x "$bin" ] || fail "not an executable file: $bin"

# The install runs inside a temporary project, so the binary path must be absolute.
case $bin in
  /*) ;;
  *) bin="$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")" ;;
esac

version=$("$bin" --version)
case $version in
  "skills "?*) ;;
  *) fail "unexpected --version output: $version" ;;
esac

help=$("$bin" --help)
case $help in
  *"Usage: skills"*) ;;
  *) fail "--help does not print the usage text" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

project=$tmp/project
mkdir -p "$project"

if ! ( cd "$project" && "$bin" add "$root/e2e/testdata/source" -a opencode -a claude-code -y >/dev/null 2>"$tmp/add.log" ); then
  cat "$tmp/add.log" >&2
  fail "local install failed"
fi

for skill in flat-skill nested-skill; do
  [ -f "$project/.agents/skills/$skill/SKILL.md" ] ||
    fail "canonical skill is missing: .agents/skills/$skill/SKILL.md"
  [ -L "$project/.claude/skills/$skill" ] ||
    fail "agent entry is not a link: .claude/skills/$skill"
  [ -f "$project/.claude/skills/$skill/SKILL.md" ] ||
    fail "agent link does not resolve: .claude/skills/$skill"
done

[ -f "$project/skills-lock.json" ] || fail "skills-lock.json was not written"
grep -q '"version": 2' "$project/skills-lock.json" ||
  fail "skills-lock.json is not version 2"

echo "smoke: ok ($version)"
