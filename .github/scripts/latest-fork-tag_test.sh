#!/usr/bin/env bash
set -euo pipefail

# Tests the inline selector logic used by the 'latest' step in .github/workflows/docker-image.yml
resolve_latest_fork_tag() {
  git tag -l --sort=-version:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+-[0-9]+$' | head -n 1
}

# Test 1: Real repo tags - fork release v7.2.127-21 wins over upstream tags like v7.3.14
latest="$(resolve_latest_fork_tag)"
if [[ "${latest}" != "v7.2.127-21" ]]; then
  echo "FAIL: expected v7.2.127-21, got ${latest}" >&2
  exit 1
fi
echo "PASS: resolve_latest_fork_tag correctly resolved ${latest}"

# Test 2: In synthetic git repo with mixed tags, numeric fork format wins
temp_repo="$(mktemp -d)"
trap 'rm -rf "${temp_repo}"' EXIT
(
  cd "${temp_repo}"
  git init -q
  git config user.name "test"
  git config user.email "test@example.com"
  git commit -q --allow-empty -m "init"
  git tag "v7.3.14"
  git tag "v7.3.12"
  git tag "v7.2.127-11"
  git tag "v7.2.127-21"
  git tag "v7.2.127-invalid"

  res="$(resolve_latest_fork_tag)"
  if [[ "${res}" != "v7.2.127-21" ]]; then
    echo "FAIL: expected v7.2.127-21 to win over v7.3.14 and v7.2.127-11, got ${res}" >&2
    exit 1
  fi

  # Test that older backfill -11 is NOT latest
  if [[ "v7.2.127-11" == "${res}" ]]; then
    echo "FAIL: v7.2.127-11 should not be considered latest" >&2
    exit 1
  fi
)
echo "PASS: synthetic tag competition verified (v7.2.127-21 is latest, v7.3.14 ignored, v7.2.127-11 not latest)"
