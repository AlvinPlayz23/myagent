#!/usr/bin/env bash
# Create and publish an annotated release tag with GoReleaser.
set +x
set -euo pipefail

die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
confirm() {
  local answer
  read -r -p "$1 [y/N] " answer || return 1
  [[ "$answer" == [yY] || "$answer" == [yY][eE][sS] ]]
}

command -v git >/dev/null 2>&1 || die "git is required."
command -v go >/dev/null 2>&1 || die "Go is required; see https://go.dev/dl/."
command -v goreleaser >/dev/null 2>&1 || die "GoReleaser is required; see https://goreleaser.com/install/."

root=$(git rev-parse --show-toplevel 2>/dev/null) || die "Run this from a git checkout."
cd "$root"
[[ -f .goreleaser.yaml ]] || die "No .goreleaser.yaml found at the repository root."
git remote get-url origin >/dev/null 2>&1 || die "The origin remote is not configured."
[[ $(git rev-parse --is-shallow-repository) == false ]] || die "A complete git history is required. Run git fetch --unshallow first."
[[ -z $(git status --porcelain) ]] || die "The working tree is not clean. Commit or stash changes first."
goreleaser check

version=${1:-}
if [[ -z "$version" ]]; then
  read -r -p "Release version (for example v0.1.9): " version || die "No version supplied."
fi
[[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]] || die "Use a version like v0.1.9 or v0.1.9-rc.1."
git check-ref-format "refs/tags/$version" >/dev/null || die "Invalid tag name."
if [[ "$version" == *-* ]]; then
  prerelease=${version#*-}
  IFS='.' read -r -a identifiers <<< "$prerelease"
  for identifier in "${identifiers[@]}"; do
    [[ ! "$identifier" =~ ^0[0-9]+$ ]] || die "Numeric prerelease identifiers cannot have leading zeros."
  done
fi

head_commit=$(git rev-parse HEAD)
local_tag=""
if git show-ref --verify --quiet "refs/tags/$version"; then
  [[ $(git cat-file -t "refs/tags/$version") == tag ]] || die "Existing tag $version is not annotated."
  [[ $(git rev-parse "refs/tags/$version^{}") == "$head_commit" ]] || die "Existing tag $version does not point to HEAD."
  local_tag=$(git rev-parse "refs/tags/$version")
fi
remote_refs=$(git ls-remote --tags origin "refs/tags/$version" "refs/tags/$version^{}") || die "Cannot check tags on origin."
remote_tag=$(printf '%s\n' "$remote_refs" | awk -v ref="refs/tags/$version" '$2 == ref {print $1}')
if [[ -n "$remote_tag" && ( -z "$local_tag" || "$remote_tag" != "$local_tag" ) ]]; then
  die "Tag $version already exists on origin and does not match the local annotated tag."
fi

printf '\nRelease %s at commit %s to origin.\n' "$version" "${head_commit:0:12}"
printf '%s\n' "GoReleaser --clean removes the existing dist directory."
printf '%s\n' "Warning: this repository's release workflow also publishes on v* tag pushes."
printf '%s\n' "Disable that workflow or otherwise ensure it will not publish this tag before continuing."
confirm "Is the tag-triggered publisher disabled for this release?" || die "Stopped to avoid two publishers racing."
confirm "Create/push the tag and publish this release?" || die "Release cancelled."

if [[ -z ${GITHUB_TOKEN:-} ]]; then
  [[ -r /dev/tty && -w /dev/tty ]] || die "Set GITHUB_TOKEN or run in an interactive terminal."
  command -v stty >/dev/null 2>&1 || die "stty is required for hidden token input."
  tty_state=$(stty -g < /dev/tty) || die "Could not inspect terminal settings."
  trap 'stty "$tty_state" < /dev/tty 2>/dev/null || true' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  stty -echo < /dev/tty || die "Could not disable terminal echo."
  printf 'GitHub token (input hidden): ' > /dev/tty
  IFS= read -r GITHUB_TOKEN < /dev/tty || die "Could not read token."
  stty "$tty_state" < /dev/tty
  trap - EXIT INT TERM
  printf '\n' > /dev/tty
  [[ -n "$GITHUB_TOKEN" ]] || die "A GitHub token is required."
fi
export -n GITHUB_TOKEN

if [[ -z "$local_tag" ]]; then
  git tag -a "$version" -m "Release $version"
fi
if [[ -z "$remote_tag" ]]; then
  git push origin "refs/tags/$version:refs/tags/$version"
fi

printf 'Publishing %s with GoReleaser...\n' "$version"
if ! GITHUB_TOKEN="$GITHUB_TOKEN" GORELEASER_CURRENT_TAG="$version" goreleaser release --clean; then
  printf 'Release failed. Tag %s may already be on origin; check for a partial GitHub release before retrying.\n' "$version" >&2
  exit 1
fi
printf 'Release %s completed.\n' "$version"
