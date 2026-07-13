#!/usr/bin/env bash
set -euo pipefail

REPO="manishiitg/mcp-agent-builder-go"
GH_USER="manishiitg"
MAIN_BRANCH="main"
WORKFLOW_NAME="Desktop DMG"

usage() {
  cat <<'EOF'
Usage:
  scripts/desktop-release.sh [--dry-run] v1.25.81

What it does:
  - switches gh to the manishiitg account
  - requires canonical origin/main with no unpublished local commits
  - verifies the version is newer than the current Latest release
  - commits the matching desktop package/package-lock version
  - generates release notes from commits since the previous published release
  - pushes main and the version tag
  - waits for the Desktop DMG workflow
  - publishes the workflow-created draft release with the generated notes
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

dry_run=false
if [[ "${1:-}" == "--dry-run" ]]; then
  dry_run=true
  shift
fi

version_arg="${1:-}"
if [[ -z "$version_arg" || "$version_arg" == "-h" || "$version_arg" == "--help" ]]; then
  usage
  exit 0
fi
[[ $# -eq 1 ]] || die "expected exactly one version argument"

if [[ ! "$version_arg" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  die "version must look like v1.25.81"
fi

require_cmd git
require_cmd gh
require_cmd node
require_cmd npm

semver_gt() {
  local candidate="${1#v}"
  local baseline="${2#v}"
  local candidate_major candidate_minor candidate_patch
  local baseline_major baseline_minor baseline_patch
  IFS=. read -r candidate_major candidate_minor candidate_patch <<<"$candidate"
  IFS=. read -r baseline_major baseline_minor baseline_patch <<<"$baseline"
  (( candidate_major > baseline_major )) ||
    (( candidate_major == baseline_major && candidate_minor > baseline_minor )) ||
    (( candidate_major == baseline_major && candidate_minor == baseline_minor && candidate_patch > baseline_patch ))
}

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

tag="$version_arg"
version="${tag#v}"

echo "==> Selecting GitHub account: $GH_USER"
gh auth switch -h github.com -u "$GH_USER" >/dev/null
active_user="$(gh api user --jq .login)"
[[ "$active_user" == "$GH_USER" ]] || die "active gh user is $active_user, expected $GH_USER"

auth_status="$(gh auth status -h github.com --active 2>&1 || true)"
if ! grep -Eq "Token scopes: .*'workflow'" <<<"$auth_status"; then
  echo "==> Refreshing gh token with workflow scope"
  gh auth refresh -h github.com -s workflow
fi

echo "==> Checking main branch"
if [[ -n "$(git status --porcelain)" ]]; then
  die "working tree is dirty; commit or stash changes before release"
fi

current_branch="$(git branch --show-current)"
if [[ "$current_branch" != "$MAIN_BRANCH" ]]; then
  git switch "$MAIN_BRANCH"
fi

git fetch origin "$MAIN_BRANCH" --tags

origin_url="$(git remote get-url origin)"
case "$origin_url" in
  git@github.com:manishiitg/mcp-agent-builder-go.git | \
    https://github.com/manishiitg/mcp-agent-builder-go.git | \
    https://github.com/manishiitg/mcp-agent-builder-go | \
    ssh://git@github.com/manishiitg/mcp-agent-builder-go.git)
    ;;
  *)
    die "origin points to $origin_url, expected the canonical $REPO repository"
    ;;
esac

# A stale local main may be safely fast-forwarded. Local-only or divergent
# commits are never pushed by the release script.
git merge --ff-only "origin/$MAIN_BRANCH"

local_head="$(git rev-parse "$MAIN_BRANCH")"
remote_head="$(git rev-parse "origin/$MAIN_BRANCH")"
[[ "$local_head" == "$remote_head" ]] || die "local main must exactly match origin/main; merge and push reviewed changes first"
echo "==> main is exactly in sync with origin/main ($local_head)"

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  die "local tag already exists: $tag"
fi
if git ls-remote --exit-code --tags origin "$tag" >/dev/null 2>&1; then
  die "remote tag already exists: $tag"
fi
if gh release view "$tag" --repo "$REPO" >/dev/null 2>&1; then
  die "GitHub release already exists: $tag"
fi

previous_tag="$(gh release view --repo "$REPO" --json tagName --jq .tagName)"
if [[ ! "$previous_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  die "Latest release tag is not plain semver: $previous_tag"
fi
semver_gt "$tag" "$previous_tag" || die "$tag must be greater than Latest release $previous_tag"

echo "==> Building changelog"
notes_file="$(mktemp)"
trap 'rm -f "$notes_file"' EXIT
{
  echo "Desktop release $tag."
  echo
  if [[ -n "$previous_tag" ]]; then
    echo "Changes since $previous_tag:"
    if ! git log --no-merges --pretty=format:'- %s (%h)' "$previous_tag"..HEAD; then
      echo "- Unable to compute changelog from $previous_tag."
    fi
    if [[ -z "$(git log --no-merges --pretty=format:%s "$previous_tag"..HEAD)" ]]; then
      echo "- No non-merge commits."
    fi
  else
    echo "Changes:"
    git log --no-merges --pretty=format:'- %s (%h)' -20
  fi
  echo
} >"$notes_file"

cat "$notes_file"

package_version="$(node -p "require('./desktop/package.json').version")"
lock_version="$(node -p "require('./desktop/package-lock.json').version")"
echo "==> Desktop metadata: package=$package_version lock=$lock_version target=$version"

if $dry_run; then
  if [[ "$package_version" != "$version" || "$lock_version" != "$version" ]]; then
    echo "==> Dry run: would update and commit desktop version metadata to $version"
  fi
  echo "==> Dry run complete; no commit, push, tag, workflow, or release was created"
  exit 0
fi

if [[ "$package_version" != "$version" || "$lock_version" != "$version" ]]; then
  echo "==> Updating desktop version metadata to $version"
  (
    cd desktop
    npm version "$version" --no-git-tag-version --allow-same-version
  )
  updated_package_version="$(node -p "require('./desktop/package.json').version")"
  updated_lock_version="$(node -p "require('./desktop/package-lock.json').version")"
  [[ "$updated_package_version" == "$version" && "$updated_lock_version" == "$version" ]] ||
    die "npm did not update both desktop version files to $version"
  git add desktop/package.json desktop/package-lock.json
  git commit -m "Bump desktop version to $version"
fi

[[ -z "$(git status --porcelain)" ]] || die "working tree became dirty while preparing release"

echo "==> Creating tag $tag"
git tag -a "$tag" -m "Release $tag"
echo "==> Atomically pushing main and $tag"
git push --atomic origin "$MAIN_BRANCH" "refs/tags/$tag"

head_sha="$(git rev-parse HEAD)"
run_id=""
echo "==> Waiting for $WORKFLOW_NAME run for $tag"
for _ in $(seq 1 60); do
  run_id="$(
    gh run list \
      --repo "$REPO" \
      --workflow "$WORKFLOW_NAME" \
      --limit 20 \
      --json databaseId,headBranch,headSha,event \
      --jq ".[] | select(.headBranch == \"$tag\" and .headSha == \"$head_sha\" and .event == \"push\") | .databaseId" |
      head -n 1
  )"
  [[ -n "$run_id" ]] && break
  sleep 5
done

[[ -n "$run_id" ]] || die "could not find release workflow run for $tag"

gh run watch "$run_id" --repo "$REPO" --exit-status

echo "==> Publishing release notes"
release_rows="$(
  gh api "repos/$REPO/releases?per_page=100" \
    --jq ".[] | select(.tag_name == \"$tag\") | [.id, .draft] | @tsv"
)"

published_id="$(awk '$2 == "false" { print $1; exit }' <<<"$release_rows")"
draft_id="$(awk '$2 == "true" { print $1; exit }' <<<"$release_rows")"

if [[ -n "$published_id" ]]; then
  gh release edit "$tag" --repo "$REPO" --title "$version" --notes-file "$notes_file" --latest
elif [[ -n "$draft_id" ]]; then
  gh api -X PATCH "repos/$REPO/releases/$draft_id" \
    -f name="$version" \
    -f body="$(cat "$notes_file")" \
    -F draft=false \
    -F prerelease=false \
    -f make_latest=true >/dev/null
else
  die "workflow completed, but no GitHub release/draft was found for $tag"
fi

echo "==> Removing duplicate drafts for $tag"
gh api "repos/$REPO/releases?per_page=100" \
  --jq ".[] | select(.tag_name == \"$tag\" and .draft == true) | .id" |
  while read -r duplicate_draft_id; do
    [[ -n "$duplicate_draft_id" ]] || continue
    gh api -X DELETE "repos/$REPO/releases/$duplicate_draft_id" >/dev/null
  done

echo "==> Verifying assets"
expected_assets=(
  "latest-mac.yml"
  "Runloop-$version-arm64-mac.zip"
  "Runloop-$version-arm64-mac.zip.blockmap"
  "Runloop-$version-arm64.dmg"
  "Runloop-$version-arm64.dmg.blockmap"
)

release_assets="$(gh release view "$tag" --repo "$REPO" --json assets --jq '.assets[].name')"
for asset in "${expected_assets[@]}"; do
  grep -Fxq "$asset" <<<"$release_assets" || die "missing release asset: $asset"
done

release_url="$(gh release view "$tag" --repo "$REPO" --json url --jq .url)"
echo "==> Published: $release_url"
