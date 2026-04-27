#!/bin/bash

# Discover and sync every repository under the a-novel and a-novel-kit GitHub
# organizations into the local workspace.
#
#   a-novel     → app/<repo>
#   a-novel-kit → kit/<repo>
#
# - Missing repos are cloned via SSH.
# - Existing repos have their default branch fast-forwarded; the user's current
#   branch and any uncommitted work are preserved (auto-stashed and re-applied
#   only when we have to switch branches, which is never — we update the default
#   branch ref directly when the user is on something else).
# - Git LFS smudging is disabled (GIT_LFS_SKIP_SMUDGE=1) — handle LFS later, per
#   repo, when actually needed.
#
# Usage: scripts/sync-repos.sh [-a <org/repo>]... [-i <org/repo>]... [-h|--help]

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# shellcheck source=lib/style.sh
. "${SCRIPT_DIR}/lib/style.sh"

# Skip pulling LFS blobs for both clone and pull operations.
export GIT_LFS_SKIP_SMUDGE=1

ORG_KIT="a-novel-kit"
DIR_KIT="kit"
ORG_APP="a-novel"
DIR_APP="app"

# Filter lists populated by -a / -i. Stored as bare strings of the form
# "org/repo". When ALLOW_LIST is non-empty it acts as a strict allow list;
# IGNORE_LIST is always a deny list and wins over any allow list entry.
ALLOW_LIST=()
IGNORE_LIST=()

# All "org/repo" names actually returned by `gh repo list` across both orgs.
# Used after the run to warn about -a / -i targets that didn't match anything.
DISCOVERED_FULL_NAMES=()

usage() {
    local prog
    prog="$(basename "$0")"
    cat <<EOF
Usage: ${prog} [OPTIONS]

Discover and sync every repository under the a-novel and a-novel-kit GitHub
organizations into the local workspace:

  a-novel       ->  app/<repo>
  a-novel-kit   ->  kit/<repo>

Missing repositories are cloned via SSH. Existing repositories have their
default branch fast-forwarded; the user's current branch and any uncommitted
work are preserved. Diverged default branches are skipped, never force-
overwritten. Git LFS smudging is disabled by default (GIT_LFS_SKIP_SMUDGE=1).

Options:
  -a <org/repo>   Add a repository to the explicit allow list. May be repeated.
                  When at least one -a is given, only repositories in the
                  allow list are synced. When no -a is given, every discovered
                  repository is synced (subject to -i exclusions).

  -i <org/repo>   Add a repository to the ignore list. May be repeated.
                  Ignored repositories are never synced. -i wins over -a — a
                  repo named in both lists is ignored, and a warning is
                  printed.

  -h, --help      Show this help and exit.

Repositories are identified by their GitHub <org>/<repo> path (for example
"a-novel-kit/golib"), not by their local directory path. Targets that do not
match any repository discovered in either org produce a warning.

Environment:
  NO_COLOR        If set, disable colored output.
  FORCE_COLOR     If set, force colored output even when stdout is not a TTY.
  GIT_LFS_SKIP_SMUDGE
                  Forced to 1 by this script for both clone and fetch.

Examples:
  # Sync everything (default).
  ${prog}

  # Only sync golib and the json-keys service.
  ${prog} -a a-novel-kit/golib -a a-novel/service-json-keys

  # Sync everything except the workflows repo.
  ${prog} -i a-novel-kit/workflows

  # Combined: sync the kit but skip assets.
  ${prog} -a a-novel-kit/golib -a a-novel-kit/jwt -i a-novel-kit/assets
EOF
}

# True iff "$1" looks like "org/repo" (no slashes elsewhere, no dots leading).
# Permissive enough for any realistic GitHub name.
__valid_full_name() {
    [[ "$1" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]
}

# __list_contains <needle> <item>...
__list_contains() {
    local needle="$1"
    shift
    local item
    for item in "$@"; do
        [ "$item" = "$needle" ] && return 0
    done
    return 1
}

# Parse CLI options.
while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help)
            usage
            exit 0
            ;;
        -a)
            if [ $# -lt 2 ]; then
                printf "error: -a requires an argument (an <org>/<repo> path)\n" >&2
                exit 2
            fi
            if ! __valid_full_name "$2"; then
                printf "error: -a argument '%s' is not in <org>/<repo> form\n" "$2" >&2
                exit 2
            fi
            ALLOW_LIST+=("$2")
            shift 2
            ;;
        -i)
            if [ $# -lt 2 ]; then
                printf "error: -i requires an argument (an <org>/<repo> path)\n" >&2
                exit 2
            fi
            if ! __valid_full_name "$2"; then
                printf "error: -i argument '%s' is not in <org>/<repo> form\n" "$2" >&2
                exit 2
            fi
            IGNORE_LIST+=("$2")
            shift 2
            ;;
        --)
            shift
            break
            ;;
        -*)
            printf "error: unknown option '%s' (run with --help)\n" "$1" >&2
            exit 2
            ;;
        *)
            printf "error: unexpected positional argument '%s' (run with --help)\n" "$1" >&2
            exit 2
            ;;
    esac
done

# Detect this workspace's own remote so we never re-clone ourselves into kit/
# (the stack repo hosts these scripts AND is part of the a-novel-kit org, so
# without this it would otherwise show up as kit/stack — a duplicate of the
# directory the script is already running from).
SELF_REMOTE_URL=""
if git -C "${ROOT_DIR}" rev-parse --is-inside-work-tree > /dev/null 2>&1; then
    SELF_REMOTE_URL="$(git -C "${ROOT_DIR}" remote get-url origin 2>/dev/null || true)"
fi

# Per-run accumulators for the end summary. Populated by sync_repo and
# update_existing_repo at the parent-shell level (subshells are kept inside
# update_existing_repo only and cannot mutate these).
total_cloned=0
total_updated=0
total_uptodate=0
total_skipped=0
total_failed=0

list_cloned=()
list_updated=()
list_uptodate=()
list_skipped=()
list_failed=()

require_cmd() {
    if ! command -v "$1" > /dev/null 2>&1; then
        log_error "required command not found: $1"
        exit 1
    fi
}

require_cmd gh
require_cmd git

if ! gh auth status > /dev/null 2>&1; then
    log_error "gh CLI is not authenticated — run 'gh auth login' first"
    exit 1
fi

# list_org_repos <org>
#   Emits TSV: name <TAB> sshUrl <TAB> defaultBranch <TAB> isArchived
list_org_repos() {
    local org="$1"
    gh repo list "$org" --limit 200 \
        --json name,sshUrl,defaultBranchRef,isArchived \
        --jq '.[] | [.name, .sshUrl, (.defaultBranchRef.name // "master"), (.isArchived | tostring)] | @tsv'
}

# clone_repo <name> <ssh_url> <target> <archived> <label>
clone_repo() {
    local name="$1" ssh_url="$2" target="$3" archived="$4" label="$5"

    if [ -e "${target}" ]; then
        log_error "${name}: ${target} exists but is not a git repository"
        list_failed+=("${label}")
        total_failed=$((total_failed + 1))
        return 1
    fi

    if git clone --quiet "${ssh_url}" "${target}"; then
        local extra=""
        [ "${archived}" = "true" ] && extra=" ${STYLE_DIM}(archived upstream)${STYLE_RESET}"
        log_success "${STYLE_BOLD}${name}${STYLE_RESET} ${STYLE_DIM}cloned${STYLE_RESET}${extra}"
        list_cloned+=("${label}")
        total_cloned=$((total_cloned + 1))
        return 0
    fi

    log_error "${name}: clone failed"
    list_failed+=("${label}")
    total_failed=$((total_failed + 1))
    return 1
}

# update_existing_repo <name> <default_branch> <target> <label>
#
# Strategy:
#   - On the default branch:           stash (if dirty) → pull --ff-only → stash pop
#   - On a different branch / detached: git fetch origin <def>:<def>
#       (this updates the local default-branch ref only when fast-forwardable;
#        the working tree, index and current branch are untouched)
#
# Special exit codes from the inner subshell are mapped to summary categories:
#   10 → up to date (nothing to do)
#   16 → diverged default branch (skipped, user must reconcile)
#    *  → failure
update_existing_repo() {
    local name="$1" default_branch="$2" target="$3" label="$4"

    local rc=0
    (
        cd "${target}"

        # Refresh remote refs and prune deleted ones.
        if ! git fetch --quiet --tags --prune origin; then
            printf "fetch failed\n" >&2
            exit 11
        fi

        local current_branch
        current_branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"

        # Make sure we have a local branch tracking origin/<default>.
        if ! git rev-parse --verify --quiet "refs/heads/${default_branch}" > /dev/null; then
            if git rev-parse --verify --quiet "refs/remotes/origin/${default_branch}" > /dev/null; then
                git branch --quiet --track "${default_branch}" "origin/${default_branch}"
            else
                printf "default branch '%s' not found on origin\n" "${default_branch}" >&2
                exit 12
            fi
        fi

        local local_sha remote_sha
        local_sha="$(git rev-parse "refs/heads/${default_branch}")"
        remote_sha="$(git rev-parse "refs/remotes/origin/${default_branch}")"
        if [ "${local_sha}" = "${remote_sha}" ]; then
            exit 10
        fi

        if [ "${current_branch}" = "${default_branch}" ]; then
            local stashed=0
            if ! git diff --quiet || ! git diff --cached --quiet; then
                if git stash push --include-untracked --quiet --message "sync-repos auto-stash"; then
                    stashed=1
                else
                    printf "failed to stash uncommitted changes\n" >&2
                    exit 13
                fi
            fi

            if ! git pull --quiet --ff-only origin "${default_branch}"; then
                printf "ff-only pull failed; %s has diverged or is non-trivial\n" "${default_branch}" >&2
                if [ "${stashed}" = 1 ]; then
                    git stash pop --quiet > /dev/null 2>&1 || true
                fi
                exit 16
            fi

            if [ "${stashed}" = 1 ]; then
                if ! git stash pop --quiet; then
                    printf "stash pop produced conflicts — resolve manually in this repo\n" >&2
                    exit 15
                fi
            fi
        else
            # Off the default branch: update its ref without touching HEAD.
            # `git fetch origin a:b` only succeeds when b can be fast-forwarded.
            if ! git fetch --quiet origin "${default_branch}:${default_branch}" 2>/dev/null; then
                printf "local '%s' has diverged from origin\n" "${default_branch}" >&2
                exit 16
            fi
        fi
    ) || rc=$?

    case "${rc}" in
        0)
            log_success "${STYLE_BOLD}${name}${STYLE_RESET} ${STYLE_DIM}updated (${default_branch})${STYLE_RESET}"
            list_updated+=("${label}")
            total_updated=$((total_updated + 1))
            ;;
        10)
            log_info "${STYLE_BOLD}${name}${STYLE_RESET} ${STYLE_DIM}up to date${STYLE_RESET}"
            list_uptodate+=("${label}")
            total_uptodate=$((total_uptodate + 1))
            ;;
        16)
            log_warn "${name}: ${default_branch} diverged — left untouched"
            list_skipped+=("${label} (diverged ${default_branch})")
            total_skipped=$((total_skipped + 1))
            ;;
        *)
            log_error "${name}: update failed (rc=${rc})"
            list_failed+=("${label}")
            total_failed=$((total_failed + 1))
            ;;
    esac
}

# sync_repo <org> <name> <ssh_url> <default_branch> <target_parent> <archived>
sync_repo() {
    local org="$1" name="$2" ssh_url="$3" default_branch="$4" target_parent="$5" archived="$6"
    local full_name="${org}/${name}"
    local target="${target_parent}/${name}"
    local label
    label="$(basename "${target_parent}")/${name}"

    DISCOVERED_FULL_NAMES+=("${full_name}")

    # Don't re-clone the workspace into itself.
    if [ -n "${SELF_REMOTE_URL}" ] && [ "${ssh_url}" = "${SELF_REMOTE_URL}" ]; then
        log_info "${STYLE_BOLD}${name}${STYLE_RESET} ${STYLE_DIM}skipped — this workspace's own repo${STYLE_RESET}"
        return 0
    fi

    # Apply ignore list (always wins over allow list).
    if [ "${#IGNORE_LIST[@]}" -gt 0 ] && __list_contains "${full_name}" "${IGNORE_LIST[@]}"; then
        log_info "${STYLE_BOLD}${name}${STYLE_RESET} ${STYLE_DIM}ignored via -i${STYLE_RESET}"
        if [ "${#ALLOW_LIST[@]}" -gt 0 ] && __list_contains "${full_name}" "${ALLOW_LIST[@]}"; then
            log_warn "${full_name}: present in both -a and -i; -i wins"
        fi
        return 0
    fi

    # Apply allow list when one was specified.
    if [ "${#ALLOW_LIST[@]}" -gt 0 ] && ! __list_contains "${full_name}" "${ALLOW_LIST[@]}"; then
        return 0
    fi

    if [ -d "${target}/.git" ]; then
        update_existing_repo "${name}" "${default_branch}" "${target}" "${label}"
    else
        clone_repo "${name}" "${ssh_url}" "${target}" "${archived}" "${label}" || true
    fi
}

# sync_org <org> <target_dir>
sync_org() {
    local org="$1" target_dir="$2"
    log_step "Syncing ${STYLE_CYAN_B}${org}${STYLE_RESET} ${STYLE_DIM}${STYLE_SYM_ARROW}${STYLE_RESET} ${STYLE_BOLD}${target_dir}/${STYLE_RESET}"

    mkdir -p "${ROOT_DIR}/${target_dir}"

    local repos
    if ! repos="$(list_org_repos "${org}")"; then
        log_error "failed to list repositories for ${org}"
        return 1
    fi

    if [ -z "${repos}" ]; then
        log_warn "no repositories found in ${org}"
        return 0
    fi

    while IFS=$'\t' read -r name ssh_url default_branch archived; do
        [ -z "${name}" ] && continue
        sync_repo "${org}" "${name}" "${ssh_url}" "${default_branch}" "${ROOT_DIR}/${target_dir}" "${archived}"
    done <<EOF
${repos}
EOF
}

# warn_unmatched_filters
#   After both orgs have been listed, complain about any -a / -i target that
#   doesn't correspond to a real discovered repo. Catches typos like
#   "a-novel/uikit-foo" before they silently become a no-op.
warn_unmatched_filters() {
    local f
    for f in "${ALLOW_LIST[@]}"; do
        if ! __list_contains "${f}" "${DISCOVERED_FULL_NAMES[@]}"; then
            log_warn "no repository matched -a ${f}"
        fi
    done
    for f in "${IGNORE_LIST[@]}"; do
        if ! __list_contains "${f}" "${DISCOVERED_FULL_NAMES[@]}"; then
            log_warn "no repository matched -i ${f}"
        fi
    done
}

print_summary() {
    log_step "Summary"
    printf "  %s%s%s cloned    %s%d%s\n" "${STYLE_GREEN_B}"  "${STYLE_SYM_OK}"   "${STYLE_RESET}" "${STYLE_BOLD}" "${total_cloned}"   "${STYLE_RESET}"
    printf "  %s%s%s updated   %s%d%s\n" "${STYLE_CYAN_B}"   "${STYLE_SYM_INFO}" "${STYLE_RESET}" "${STYLE_BOLD}" "${total_updated}"  "${STYLE_RESET}"
    printf "  %s%s%s up-to-date %s%d%s\n" "${STYLE_DIM}"     "${STYLE_SYM_BULLET}" "${STYLE_RESET}" "${STYLE_BOLD}" "${total_uptodate}" "${STYLE_RESET}"
    printf "  %s%s%s skipped   %s%d%s\n" "${STYLE_YELLOW_B}" "${STYLE_SYM_WARN}" "${STYLE_RESET}" "${STYLE_BOLD}" "${total_skipped}"  "${STYLE_RESET}"
    printf "  %s%s%s failed    %s%d%s\n" "${STYLE_RED_B}"    "${STYLE_SYM_ERR}"  "${STYLE_RESET}" "${STYLE_BOLD}" "${total_failed}"   "${STYLE_RESET}"

    if [ "${#list_skipped[@]}" -gt 0 ]; then
        printf "\n  %sSkipped:%s\n" "${STYLE_YELLOW}" "${STYLE_RESET}"
        for entry in "${list_skipped[@]}"; do
            printf "    %s%s%s %s\n" "${STYLE_YELLOW}" "${STYLE_SYM_BULLET}" "${STYLE_RESET}" "${entry}"
        done
    fi

    if [ "${#list_failed[@]}" -gt 0 ]; then
        printf "\n  %sFailed:%s\n" "${STYLE_RED}" "${STYLE_RESET}"
        for entry in "${list_failed[@]}"; do
            printf "    %s%s%s %s\n" "${STYLE_RED}" "${STYLE_SYM_ERR}" "${STYLE_RESET}" "${entry}"
        done
    fi
    printf "\n"
    separator
}

banner "Repository Sync"
log_dim "Discovering and syncing repositories under ${ORG_APP} and ${ORG_KIT}."
log_dim "GIT_LFS_SKIP_SMUDGE=1 — LFS blobs will not be downloaded."
if [ "${#ALLOW_LIST[@]}" -gt 0 ]; then
    log_dim "Allow list (-a): ${ALLOW_LIST[*]}"
fi
if [ "${#IGNORE_LIST[@]}" -gt 0 ]; then
    log_dim "Ignore list (-i): ${IGNORE_LIST[*]}"
fi

sync_org "${ORG_KIT}" "${DIR_KIT}"
sync_org "${ORG_APP}" "${DIR_APP}"

warn_unmatched_filters

print_summary

if [ "${total_failed}" -gt 0 ]; then
    exit 1
fi
