#!/usr/bin/env bash
set -euo pipefail

REPO="manishiitg/mcp-agent-builder-go"
GH_USER="manishiitg"
MAIN_BRANCH="main"
WORKFLOW_NAME="Desktop DMG"

usage() {
  cat <<'EOF'
Usage:
  scripts/desktop-release.sh v1.25.81

What it does:
  - switches gh to the manishiitg account
  - requires the release to come from main
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

version_arg="${1:-}"
if [[ -z "$version_arg" || "$version_arg" == "-h" || "$version_arg" == "--help" ]]; then
  usage
  exit 0
fi

if [[ ! "$version_arg" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  die "version must look like v1.25.81"
fi

require_cmd git
require_cmd gh

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

local_head="$(git rev-parse "$MAIN_BRANCH")"
remote_head="$(git rev-parse "origin/$MAIN_BRANCH")"
merge_base="$(git merge-base "$MAIN_BRANCH" "origin/$MAIN_BRANCH")"

if [[ "$local_head" == "$remote_head" ]]; then
  echo "==> main is in sync with origin/main"
elif [[ "$merge_base" == "$remote_head" ]]; then
  echo "==> main is ahead of origin/main; will push before tagging"
else
  die "main is behind or diverged from origin/main; sync it first"
fi

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  die "local tag already exists: $tag"
fi
if git ls-remote --exit-code --tags origin "$tag" >/dev/null 2>&1; then
  die "remote tag already exists: $tag"
fi

echo "==> Building changelog"
previous_tag="$(
  gh api "repos/$REPO/releases?per_page=100" \
    --jq '.[] | select(.draft == false and .prerelease == false) | .tag_name' |
    grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' |
    grep -v -x "$tag" |
    head -n 1 || true
)"

notes_file="$(mktemp)"
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

echo "==> Pushing main"
git push origin "$MAIN_BRANCH"

echo "==> Creating and pushing tag $tag"
git tag -a "$tag" -m "Release $tag"
git push origin "$tag"

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
