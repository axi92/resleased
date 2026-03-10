#!/bin/bash

# Get the previous tag to determine commit range
PREV_TAG=$(git tag --sort=-version:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sed -n '2p')
CURRENT_TAG=${{ github.ref_name }}

if [ -z "$PREV_TAG" ]; then
  RANGE="$CURRENT_TAG"
  echo "No previous tag found, using all commits up to $CURRENT_TAG"
else
  RANGE="${PREV_TAG}..${CURRENT_TAG}"
  echo "Generating notes for range: $RANGE"
fi

# Parse conventional commits into sections
{
  echo "## What's Changed"
  echo ""

  BREAKING=$(git log $RANGE --pretty=format:"%s" | grep -E '^(feat|fix|refactor|perf)(\(.+\))?!:' || true)
  if [ -n "$BREAKING" ]; then
    echo "### 💥 Breaking Changes"
    echo "$BREAKING" | while IFS= read -r line; do echo "- $line"; done
    echo ""
  fi

  FEATURES=$(git log $RANGE --pretty=format:"%s" | grep -E '^feat(\(.+\))?:' || true)
  if [ -n "$FEATURES" ]; then
    echo "### ✨ Features"
    echo "$FEATURES" | while IFS= read -r line; do echo "- $line"; done
    echo ""
  fi

  FIXES=$(git log $RANGE --pretty=format:"%s" | grep -E '^fix(\(.+\))?:' || true)
  if [ -n "$FIXES" ]; then
    echo "### 🐛 Bug Fixes"
    echo "$FIXES" | while IFS= read -r line; do echo "- $line"; done
    echo ""
  fi

  PERF=$(git log $RANGE --pretty=format:"%s" | grep -E '^perf(\(.+\))?:' || true)
  if [ -n "$PERF" ]; then
    echo "### ⚡ Performance"
    echo "$PERF" | while IFS= read -r line; do echo "- $line"; done
    echo ""
  fi

  REFACTOR=$(git log $RANGE --pretty=format:"%s" | grep -E '^refactor(\(.+\))?:' || true)
  if [ -n "$REFACTOR" ]; then
    echo "### ♻️ Refactoring"
    echo "$REFACTOR" | while IFS= read -r line; do echo "- $line"; done
    echo ""
  fi

  DOCS=$(git log $RANGE --pretty=format:"%s" | grep -E '^docs(\(.+\))?:' || true)
  if [ -n "$DOCS" ]; then
    echo "### 📖 Documentation"
    echo "$DOCS" | while IFS= read -r line; do echo "- $line"; done
    echo ""
  fi

  CHORE=$(git log $RANGE --pretty=format:"%s" | grep -E '^(chore|ci|build|test)(\(.+\))?:' || true)
  if [ -n "$CHORE" ]; then
    echo "### 🔧 Chores"
    echo "$CHORE" | while IFS= read -r line; do echo "- $line"; done
    echo ""
  fi

  if [ -n "$PREV_TAG" ]; then
    echo "**Full Changelog**: https://github.com/${{ github.repository }}/compare/${PREV_TAG}...${CURRENT_TAG}"
  fi
} > release_notes.md

cat release_notes.md