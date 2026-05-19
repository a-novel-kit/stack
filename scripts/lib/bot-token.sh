#!/bin/bash

# GitHub App installation-token helper for the a-novel ecosystem bots.
# Source this file (do not execute it):
#
#   . "$(dirname "$0")/lib/bot-token.sh"
#   GH_TOKEN="$(bot_token a-novel)" gh pr comment 549 --body "..."
#   bot_gh a-novel pr comment 549 --body "..."   # equivalent shorthand
#
# Org → App ID, Installation ID, pem filename are configured in bots.conf.
# Private keys default to <repo-root>/.secrets/<file>.pem; override the
# directory (not the filenames) with BOT_KEY_DIR.

[ -n "${ANOVEL_BOT_TOKEN_LOADED:-}" ] && return 0
ANOVEL_BOT_TOKEN_LOADED=1

__BOT_TOKEN_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

. "${__BOT_TOKEN_LIB_DIR}/style.sh"

# bots.conf uses associative arrays (bash 4+). Guard explicitly so older bash
# (e.g. macOS /bin/bash 3.2) gets a clear message instead of "declare: -A:
# invalid option".
if [ -z "${BASH_VERSION:-}" ] || [ "${BASH_VERSINFO[0]:-0}" -lt 4 ]; then
	log_error "bot-token.sh requires bash 4+ (associative arrays in bots.conf)"
	log_dim  "      macOS ships bash 3.2 at /bin/bash; install a newer bash via Homebrew"
	return 2
fi

# Source bots.conf through tr so CRLF line endings (introduced by editors that
# default to Windows newlines) don't break the sourced declarations at runtime.
# .gitattributes normalizes on commit, but the working-tree file is what bash
# actually reads.
eval "$(tr -d '\r' < "${__BOT_TOKEN_LIB_DIR}/bots.conf")"

__bot_require_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		log_error "bot-token.sh: required command not found: $1"
		return 1
	fi
}

__bot_require_cmd openssl || return $?
__bot_require_cmd curl    || return $?
__bot_require_cmd jq      || return $?

__bot_repo_root() {
	# Repo root is two levels above scripts/lib/, regardless of caller cwd.
	(cd "${__BOT_TOKEN_LIB_DIR}/../.." && pwd)
}

__bot_b64url() {
	# RFC 7515 §2 base64url: URL-safe alphabet, no padding.
	openssl base64 -A | tr '+/' '-_' | tr -d '='
}

# bot_token <org>
# Prints a short-lived (1h) GitHub App installation token to stdout.
# Returns non-zero on misconfiguration, missing key, or API failure.
bot_token() {
	local org="${1:-}"
	if [ -z "$org" ]; then
		log_error "bot_token: missing org argument (e.g. a-novel, a-novel-kit)"
		return 2
	fi

	local app_id="${BOT_APP_IDS[$org]:-}"
	local installation_id="${BOT_INSTALLATION_IDS[$org]:-}"
	local key_file="${BOT_KEY_FILES[$org]:-}"
	if [ -z "$app_id" ] || [ -z "$installation_id" ] || [ -z "$key_file" ]; then
		log_error "bot_token: org '$org' is not configured in bots.conf"
		return 2
	fi

	local key_dir="${BOT_KEY_DIR:-$(__bot_repo_root)/.secrets}"
	local pem="${key_dir}/${key_file}"
	if [ ! -r "$pem" ]; then
		log_error "bot_token: cannot read private key at $pem"
		log_dim  "      set BOT_KEY_DIR or place the key at the default location"
		return 2
	fi

	# Non-blocking permission warning: a .pem with group/other access lets any
	# local user mint installation tokens. stat -c is GNU, -f %Lp is BSD.
	local mode
	mode=$(stat -c %a "$pem" 2>/dev/null || stat -f %Lp "$pem" 2>/dev/null)
	case "$mode" in
		"")  ;;
		*00) ;;
		*)   log_warn "bot_token: $pem mode is $mode; recommend: chmod 600 '$pem'" ;;
	esac

	local now header payload signature jwt response token
	now=$(date +%s)
	header=$(printf '%s' '{"alg":"RS256","typ":"JWT"}' | __bot_b64url)
	# iat -60s tolerates small clock skew; exp +540s stays under GitHub's 600s cap.
	payload=$(printf '{"iat":%d,"exp":%d,"iss":"%s"}' \
		$((now - 60)) $((now + 540)) "$app_id" | __bot_b64url)
	signature=$(printf '%s' "${header}.${payload}" \
		| openssl dgst -sha256 -sign "$pem" \
		| __bot_b64url)
	jwt="${header}.${payload}.${signature}"

	response=$(curl -sS --max-time 30 -X POST \
		-H "Authorization: Bearer ${jwt}" \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" \
		"https://api.github.com/app/installations/${installation_id}/access_tokens")

	token=$(printf '%s' "$response" | jq -r '.token // empty')
	if [ -z "$token" ]; then
		log_error "bot_token: failed to mint installation token for $org"
		log_dim  "      $(printf '%s' "$response" | jq -r '.message // .' 2>/dev/null)"
		return 1
	fi

	printf '%s' "$token"
}

# __bot_gh_deny refuses any gh subcommand that AUTHORS a PR/issue or changes
# its state. The bot account is comment-only: PR creation/edits must run with
# the operator's user token so CI triggers and auto-assign-author / CODEOWNERS
# work (see .claude/skills/open-pull-request/SKILL.md §5.0). A doc rule alone
# did not hold — this makes the misuse mechanically impossible regardless of
# which skill context the caller is in.
__bot_gh_deny() {
	case "${1:-} ${2:-}" in
	"pr create" | "pr edit" | "pr ready" | "pr merge" | "pr close" | \
		"pr reopen" | "pr review" | "pr lock" | "pr unlock" | \
		"issue create" | "issue edit" | "issue close" | "issue reopen" | \
		"issue transfer" | "issue pin" | "issue lock" | "issue unlock")
		log_error "bot_gh: '$1 $2' is not allowed — the bot account is comment-only."
		log_dim "      Run it with the operator user token instead: plain \`gh $1 $2 …\`"
		log_dim "      (see .claude/skills/open-pull-request/SKILL.md §5.0)."
		return 3
		;;
	esac
}

# bot_gh <org> <gh args...>
# Runs `gh` with GH_TOKEN set to a fresh installation token for <org>.
# Reserved for COMMENTS only (top-level PR/issue comments, review-thread
# replies). PR/issue authoring & state changes are denied — see __bot_gh_deny.
bot_gh() {
	local org="${1:-}"
	if [ -z "$org" ]; then
		log_error "bot_gh: missing org argument"
		return 2
	fi
	shift

	__bot_gh_deny "$@" || return $?

	local token
	token=$(bot_token "$org") || return $?
	GH_TOKEN="$token" gh "$@"
}
