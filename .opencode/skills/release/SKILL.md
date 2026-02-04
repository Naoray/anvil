---
name: release
description: Prepare and publish releases with version analysis, documentation updates, and quality checks
license: MIT
compatibility: opencode
metadata:
  audience: maintainers
  workflow: github
---

# Release Management Skill

## Overview

This skill prepares and publishes a new release of the Arbor project. It automates version determination, documentation updates, quality checks, and release verification.

**Trigger:** `/release`

## Prerequisites

Before running this skill:
- All feature changes must be committed
- Working directory must be clean
- You have push access to the repository
- `gh` CLI tool is installed and authenticated

## Workflow

### Step 1: Pre-flight Checks

Verify repository is ready for release:

```bash
# Fetch latest tags from remote to ensure we have all version information
git fetch --tags

# Check for uncommitted changes
if [[ -n $(git status --porcelain) ]]; then
    echo "Error: Working directory has uncommitted changes"
    echo "Please commit all changes before releasing"
    exit 1
fi

# Check current branch
CURRENT_BRANCH=$(git branch --show-current)
if [[ "$CURRENT_BRANCH" != "main" ]]; then
    echo "Warning: Currently on branch '$CURRENT_BRANCH' (not main)"
    # Ask user if they want to continue
fi
```

### Step 2: Determine Version Number

Analyze changes and recommend version bump:

```bash
# Fetch latest tags from remote to ensure we have all version information
git fetch --tags

# Get the latest tag by creation date (most recent release)
CURRENT_VERSION=$(git tag -l --sort=-creatordate | head -1)
if [[ -z "$CURRENT_VERSION" ]]; then
    CURRENT_VERSION="v0.0.0"
fi
echo "Latest tag: $CURRENT_VERSION"

# Check if this tag is an ancestor of HEAD
if git merge-base --is-ancestor $CURRENT_VERSION HEAD 2>/dev/null; then
    echo "  $CURRENT_VERSION is an ancestor of current HEAD"
else
    echo "  Warning: $CURRENT_VERSION is NOT an ancestor of current HEAD"
    echo "  This may indicate the tag was created on a different branch"
fi

# Show recent tags for context
echo ""
echo "Recent tags:"
git tag -l --sort=-creatordate | head -5 | while read tag; do
    if git merge-base --is-ancestor $tag HEAD 2>/dev/null; then
        echo "  $tag (on current branch)"
    else
        echo "  $tag (not on current branch)"
    fi
done

# Get commits since last tag
# Handle both cases: tag is ancestor or not
if git merge-base --is-ancestor $CURRENT_VERSION HEAD 2>/dev/null; then
    # Tag is ancestor, get commits after it
    COMMITS=$(git log ${CURRENT_VERSION}..HEAD --oneline)
else
    # Tag is not ancestor, get all commits on current branch
    COMMITS=$(git log --oneline)
fi

# Categorize commits
BREAKING=$(echo "$COMMITS" | grep -i "break\|breaking" || true)
FEATURES=$(echo "$COMMITS" | grep -i "feat\|feature" || true)
FIXES=$(echo "$COMMITS" | grep -i "fix\|bug" || true)

# Extract version components
VERSION_NUM=${CURRENT_VERSION#v}
MAJOR=$(echo $VERSION_NUM | cut -d. -f1)
MINOR=$(echo $VERSION_NUM | cut -d. -f2)
PATCH=$(echo $VERSION_NUM | cut -d. -f3)

# Determine recommendations
if [[ -n $BREAKING ]]; then
    SUGGESTED_MAJOR="v$((MAJOR + 1)).0.0"
    SUGGESTED_MINOR=""
    SUGGESTED_PATCH=""
    RECOMMENDATION="MAJOR"
    REASON="Breaking changes detected"
elif [[ -n $FEATURES ]]; then
    SUGGESTED_MAJOR=""
    SUGGESTED_MINOR="v${MAJOR}.$((MINOR + 1)).0"
    SUGGESTED_PATCH=""
    RECOMMENDATION="MINOR"
    REASON="New features added"
else
    SUGGESTED_MAJOR=""
    SUGGESTED_MINOR=""
    SUGGESTED_PATCH="v${MAJOR}.${MINOR}.$((PATCH + 1))"
    RECOMMENDATION="PATCH"
    REASON="Bug fixes only"
fi
```

Present options to user:

```
Version bump recommendations based on changes since CURRENT_VERSION:

Recommendation: RECOMMENDATION (REASON)

Options:
1. MAJOR: SUGGESTED_MAJOR (breaking changes - backward incompatible)
2. MINOR: SUGGESTED_MINOR (new features - backward compatible)
3. PATCH: SUGGESTED_PATCH (bug fixes only)
4. Custom: Enter your own version

Which version would you like to release? (1-4)
```

### Step 3: Run Quality Gates

All checks must pass before proceeding:

```bash
echo "Running quality gates..."

# Run all tests
echo "→ Running unit tests..."
go test ./... -v || exit 1

# Run race detector
echo "→ Running race detector..."
go test ./... -race || exit 1

# Verify build
echo "→ Verifying build..."
go build ./... || exit 1

# Run vet
echo "→ Running go vet..."
go vet ./... || exit 1

echo "✓ All quality gates passed"
```

### Step 4: Update CHANGELOG.md

Read current CHANGELOG and add new entry at top:

**Format:** Keep a Changelog (https://keepachangelog.com/en/1.0.0/)

Categories:
- **Added** - New features
- **Changed** - Changes to existing functionality
- **Deprecated** - Soon-to-be removed features
- **Removed** - Now removed features
- **Fixed** - Bug fixes
- **Security** - Security-related changes

**Content Guidelines:**
- Major features: Brief description
- Minor changes: One-line summary
- Bug fixes: One-line summary
- No practical examples
- Use present tense ("Add" not "Added")
- Group related changes

Example:

```markdown
## [0.4.2] - 2026-01-31

### Added
- Output capture support for scaffold steps via `store_as` option
- Support for capturing output from all binary steps

### Fixed
- Race condition in env.write step
- Database naming with hyphenated branch names

### Changed
- Improved template variable error messages
```

Update version links at bottom:

```markdown
[0.4.2]: https://github.com/artisanexperiences/arbor/compare/v0.4.1...v0.4.2
```

### Step 5: Update README.md

Review and update documentation:

**Areas to Review:**
1. Command examples
2. Configuration examples
3. Feature lists
4. Step documentation
5. Template variables table
6. Installation instructions

**Updates:**
- Add new features to appropriate sections
- Add usage examples for new features
- Update changed behavior
- Remove deprecated features
- Ensure consistency with code

### Step 6: Draft or Final Release

Ask user:

```
Would you like to:
1. Create draft release (can publish after review)
2. Create and publish release immediately
```

### Step 7: Commit and Tag

Stage and commit documentation:

```bash
git add CHANGELOG.md README.md
git commit -m "release: prepare vNEW_VERSION

Summary of changes:
- List key changes here

See CHANGELOG.md for full details."
```

Create annotated tag:

```bash
git tag -a vNEW_VERSION -m "Release vNEW_VERSION

Brief description of this release.

See CHANGELOG.md for full details."
```

Push to origin:

```bash
git push origin main && git push origin vNEW_VERSION
```

### Step 8: Verify Release

Monitor CI/CD and verify release:

```bash
# Check workflow status
echo "Monitoring CI/CD workflow..."
sleep 10  # Wait for workflow to start

# Get workflow status using gh CLI
gh run list --workflow=release.yml --limit 1

# Poll until complete (timeout after 10 minutes)
for i in {1..60}; do
    STATUS=$(gh run list --workflow=release.yml --limit 1 --json status --jq '.[0].status')
    if [[ "$STATUS" == "completed" ]]; then
        CONCLUSION=$(gh run list --workflow=release.yml --limit 1 --json conclusion --jq '.[0].conclusion')
        if [[ "$CONCLUSION" == "success" ]]; then
            echo "✓ Release workflow completed successfully"
            break
        else
            echo "✗ Release workflow failed"
            exit 1
        fi
    fi
    echo "Workflow still running... ($i/60)"
    sleep 10
done

# Verify GitHub release was created
gh release view vNEW_VERSION

if [[ $? -eq 0 ]]; then
    echo "✓ Release vNEW_VERSION published successfully"
    echo "Download URL: https://github.com/artisanexperiences/arbor/releases/tag/vNEW_VERSION"
else
    echo "✗ Failed to verify release"
    exit 1
fi
```

## Notes

- Version format: Always use `v` prefix (e.g., v0.4.1)
- Tests must pass before release is tagged
- CHANGELOG entries should be concise, examples go in README
- CI/CD runs automatically on tag push
- Use `gh` CLI to monitor release progress
