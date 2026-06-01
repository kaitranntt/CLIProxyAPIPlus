#!/usr/bin/env bash
#
# sync-upstream.sh
# Fetch the upstream main branch (with prune) and sync it into the current branch.
#
# Usage:
#   ./sync-upstream.sh                # fetch upstream/main and merge into current branch
#   ./sync-upstream.sh --rebase       # rebase current branch onto upstream/main instead of merging
#   REMOTE=upstream BRANCH=main ./sync-upstream.sh   # override remote/branch via env
#
set -euo pipefail

REMOTE="${REMOTE:-upstream}"
BRANCH="${BRANCH:-main}"
MODE="merge"

for arg in "$@"; do
  case "$arg" in
    --rebase) MODE="rebase" ;;
    --merge)  MODE="merge" ;;
    -h|--help)
      echo "Usage: $0 [--merge|--rebase]"
      echo "  REMOTE=$REMOTE BRANCH=$BRANCH (override via env vars)"
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      exit 1
      ;;
  esac
done

# Must be inside a git work tree.
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "Error: not inside a git repository." >&2
  exit 1
fi

# Confirm the remote exists.
if ! git remote get-url "$REMOTE" >/dev/null 2>&1; then
  echo "Error: remote '$REMOTE' not found. Available remotes:" >&2
  git remote -v >&2
  exit 1
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$CURRENT_BRANCH" = "HEAD" ]; then
  echo "Error: detached HEAD state. Checkout a branch first." >&2
  exit 1
fi

# Warn if there are uncommitted changes.
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "Error: you have uncommitted changes. Commit or stash them first." >&2
  git status -s >&2
  exit 1
fi

echo ">> Fetching $REMOTE/$BRANCH (with prune)..."
git fetch -p "$REMOTE" "$BRANCH"

echo ">> Syncing $REMOTE/$BRANCH into '$CURRENT_BRANCH' (mode: $MODE)..."
if [ "$MODE" = "rebase" ]; then
  git rebase "$REMOTE/$BRANCH"
else
  git merge "$REMOTE/$BRANCH"
fi

echo ">> Done. '$CURRENT_BRANCH' is now synced with $REMOTE/$BRANCH."
