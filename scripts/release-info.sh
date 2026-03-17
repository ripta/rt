#!/usr/bin/env bash

set -euo pipefail

if [ -n "$(git status --porcelain)" ]; then
  echo "error: working tree is dirty; commit or stash changes first" >&2
  exit 1
fi

latest_tag=$(git describe --tags --abbrev=0 2>/dev/null) || {
  echo "error: no tags found" >&2
  exit 1
}

commit_count=$(git rev-list "${latest_tag}..HEAD" --count)
if [ "$commit_count" -eq 0 ]; then
  echo "error: no commits since ${latest_tag}" >&2
  exit 1
fi

today=$(date +%Y-%m-%d)
year=$(date +%Y)
month=$(date +%m)

# Find highest serial for current YYYY.MM tags
highest_serial=0
for tag in $(git tag -l "v${year}.${month}.*"); do
  serial="${tag##*.}"
  serial=$((10#${serial}))
  if [ "$serial" -gt "$highest_serial" ]; then
    highest_serial=$serial
  fi
done

next_serial=$((highest_serial + 1))
next_tag=$(printf "v%s.%s.%02d" "$year" "$month" "$next_serial")

git_log=$(git log "${latest_tag}..HEAD" --format='%s%n%b---')

echo "@@LATEST_TAG"
echo "$latest_tag"
echo "@@NEXT_TAG"
echo "$next_tag"
echo "@@TODAY"
echo "$today"
echo "@@COMMIT_COUNT"
echo "$commit_count"
echo "@@GIT_LOG"
echo "$git_log"
echo "@@END"
