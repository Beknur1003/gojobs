#!/usr/bin/env bash
# One daily run: collect vacancies, rebuild the site, publish it.
# launchd runs this (see `make install-daily`); it is also fine to run by hand.
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p logs
exec >>logs/daily.log 2>&1
echo "=== $(date '+%F %T')"

export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/go/bin:$PATH"

go run ./cmd/gojobs run

git add data docs
if git diff --cached --quiet; then
  echo "nothing changed"
  exit 0
fi
git commit -q -m "Update vacancies $(date '+%F')"

if git remote get-url origin >/dev/null 2>&1; then
  git push -q origin HEAD
  echo "published"
else
  echo "no git remote yet: committed locally only"
fi
