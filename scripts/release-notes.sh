#!/bin/sh
set -e

if [ -z "$1" ]; then
  echo "Error: Release tag not provided." >&2
  exit 1
fi

RELEASE_TAG="$1"

# Verify we're in a git repository
if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "Error: Not a git repository." >&2
  exit 1
fi

# Verify the release tag exists
if ! git rev-parse --verify "$RELEASE_TAG" >/dev/null 2>&1; then
  echo "Error: Tag '$RELEASE_TAG' not found." >&2
  exit 1
fi

# --------------------------------------------------------------------------
# 1. Find the previous tag (version-sorted)
# --------------------------------------------------------------------------
PREV_TAG=""

# Build a list of all tags sorted by version ascending. Use version:refname
# so that v0.2.0 < v0.10.0 etc.
ALL_TAGS=$(git tag --sort=version:refname 2>/dev/null || true)

if [ -n "$ALL_TAGS" ]; then
  # Walk the list to find the tag immediately before the release tag
  LAST=""
  FOUND=0
  IFS='
'
  for TAG in $ALL_TAGS; do
    if [ "$TAG" = "$RELEASE_TAG" ]; then
      PREV_TAG="$LAST"
      FOUND=1
      break
    fi
    LAST="$TAG"
  done
  unset IFS

  # If the release tag wasn't in the list (e.g. it's a branch or HEAD),
  # find the most recent tag that is an ancestor of the given ref.
  if [ "$FOUND" -eq 0 ]; then
    PREV_TAG=$(git describe --tags --abbrev=0 "$RELEASE_TAG" 2>/dev/null || echo "")
  fi
fi

# If the previous tag ended up equal to the release tag (edge case), discard it
if [ "$PREV_TAG" = "$RELEASE_TAG" ]; then
  PREV_TAG=""
fi

# --------------------------------------------------------------------------
# 2. Collect commit messages from git log
# --------------------------------------------------------------------------
if [ -n "$PREV_TAG" ]; then
  COMMITS=$(git log --oneline --no-decorate "${PREV_TAG}..${RELEASE_TAG}" 2>/dev/null || true)
else
  # No previous tag – show all commits up to the release tag
  COMMITS=$(git log --oneline --no-decorate "$RELEASE_TAG" 2>/dev/null || true)
fi

if [ -z "$COMMITS" ]; then
  echo "# Release Notes for $RELEASE_TAG"
  echo
  echo "No commits found."
  exit 0
fi

# --------------------------------------------------------------------------
# 3. Categorize commits by conventional-commit prefix
# --------------------------------------------------------------------------
# We write each category to a temp file to avoid subshell scoping issues.
tmpdir="${TMPDIR:-/tmp}"
feat_file="${tmpdir}/relnote_feat.$$"
fix_file="${tmpdir}/relnote_fix.$$"
docs_file="${tmpdir}/relnote_docs.$$"
perf_file="${tmpdir}/relnote_perf.$$"
refactor_file="${tmpdir}/relnote_refactor.$$"
test_file="${tmpdir}/relnote_test.$$"
chore_file="${tmpdir}/relnote_chore.$$"
other_file="${tmpdir}/relnote_other.$$"

# Clean up temp files on exit
trap 'rm -f "$feat_file" "$fix_file" "$docs_file" "$perf_file" \
             "$refactor_file" "$test_file" "$chore_file" "$other_file"' EXIT

# Initialize empty category files
: > "$feat_file"
: > "$fix_file"
: > "$docs_file"
: > "$perf_file"
: > "$refactor_file"
: > "$test_file"
: > "$chore_file"
: > "$other_file"

echo "$COMMITS" | while IFS= read -r line; do
  [ -z "$line" ] && continue

  # Strip the commit SHA (everything up to and including the first space)
  msg="${line#* }"

  # Determine category by conventional-commit prefix (with optional scope)
  case "$msg" in
    feat:*|feat\(*:*)      echo "  - $msg" >> "$feat_file" ;;
    fix:*|fix\(*:*)        echo "  - $msg" >> "$fix_file" ;;
    docs:*|docs\(*:*)      echo "  - $msg" >> "$docs_file" ;;
    perf:*|perf\(*:*)      echo "  - $msg" >> "$perf_file" ;;
    refactor:*|refactor\(*:*) echo "  - $msg" >> "$refactor_file" ;;
    test:*|test\(*:*)      echo "  - $msg" >> "$test_file" ;;
    chore:*|chore\(*:*)    echo "  - $msg" >> "$chore_file" ;;
    *)                     echo "  - $msg" >> "$other_file" ;;
  esac
done

# Count total commits (independent of the pipe subshell)
TOTAL=$(echo "$COMMITS" | wc -l | tr -d ' ')

# --------------------------------------------------------------------------
# 4. Generate the release notes
# --------------------------------------------------------------------------
echo "# Release Notes for $RELEASE_TAG"
echo

# Summary line
if [ "$TOTAL" -eq 1 ]; then
  echo "This release includes 1 commit."
else
  echo "This release includes $TOTAL commits."
fi

if [ -n "$PREV_TAG" ]; then
  echo "Changes since $PREV_TAG:"
else
  echo "Initial release."
fi
echo

# Helper to print a section if it has content
print_section() {
  label="$1"
  file="$2"
  if [ -s "$file" ]; then
    echo "### $label"
    echo
    cat "$file"
    echo
  fi
}

print_section "Features"        "$feat_file"
print_section "Bug Fixes"       "$fix_file"
print_section "Documentation"   "$docs_file"
print_section "Performance"     "$perf_file"
print_section "Refactoring"     "$refactor_file"
print_section "Testing"         "$test_file"
print_section "Chores"          "$chore_file"
print_section "Other Changes"   "$other_file"
