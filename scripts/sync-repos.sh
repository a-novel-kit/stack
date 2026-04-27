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
# Usage: scripts/sync-repos.sh

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

# sync_repo <name> <ssh_url> <default_branch> <target_parent> <archived>
sync_repo() {
    local name="$1" ssh_url="$2" default_branch="$3" target_parent="$4" archived="$5"
    local target="${target_parent}/${name}"
    local label
    label="$(basename "${target_parent}")/${name}"

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
        sync_repo "${name}" "${ssh_url}" "${default_branch}" "${ROOT_DIR}/${target_dir}" "${archived}"
    done <<EOF
${repos}
EOF
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

sync_org "${ORG_KIT}" "${DIR_KIT}"
sync_org "${ORG_APP}" "${DIR_APP}"

print_summary

if [ "${total_failed}" -gt 0 ]; then
    exit 1
fi
