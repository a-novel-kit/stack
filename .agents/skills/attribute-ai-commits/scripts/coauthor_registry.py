#!/usr/bin/env python3
"""Read the co-author registry or verify a candidate GitHub identity."""

from __future__ import annotations

import argparse
import html
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import date
from pathlib import Path
from typing import Any


REGISTRY = Path(__file__).resolve().parent.parent / "references" / "providers.json"
USER_AGENT = "a-novel-ai-coauthor-verifier/1"
NOREPLY_PATTERN = re.compile(
    r"^(?P<id>[0-9]+)\+(?P<login>.+)@users\.noreply\.github\.com$",
    re.IGNORECASE,
)
GITHUB_COMMIT_PATTERN = re.compile(
    r"^https://github\.com/(?P<repository>[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/commit/"
    r"(?P<commit>[0-9a-fA-F]{40})/?$"
)


class RegistryError(RuntimeError):
    """Report an operational error separately from an unavailable identity."""


def read_json(url: str) -> dict[str, Any]:
    request = urllib.request.Request(
        url,
        headers={"Accept": "application/vnd.github+json", "User-Agent": USER_AGENT},
    )
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            return json.load(response)
    except (OSError, urllib.error.URLError, json.JSONDecodeError) as error:
        raise RegistryError(f"could not read {url}: {error}") from error


def read_text(url: str) -> str:
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            return response.read().decode("utf-8")
    except (OSError, urllib.error.URLError, UnicodeDecodeError) as error:
        raise RegistryError(f"could not read {url}: {error}") from error


def load_registry(path: Path) -> dict[str, Any]:
    try:
        registry = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise RegistryError(f"could not read registry {path}: {error}") from error
    if registry.get("schema") != 1 or not isinstance(registry.get("providers"), dict):
        raise RegistryError(f"unsupported registry schema in {path}")
    if not registry.get("scope") or not registry.get("as_of"):
        raise RegistryError(f"registry scope or as_of date missing in {path}")
    return registry


def resolve_provider(registry: dict[str, Any], requested: str) -> tuple[str, dict[str, Any]] | None:
    normalized = requested.casefold()
    for provider, entry in registry["providers"].items():
        aliases = [provider, *entry.get("aliases", [])]
        if normalized in {alias.casefold() for alias in aliases}:
            return provider, entry
    return None


def validate_registry(registry: dict[str, Any]) -> dict[str, int]:
    seen_names: dict[str, str] = {}
    counts = {"verified": 0, "unavailable": 0}
    for provider, entry in registry["providers"].items():
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", provider):
            raise RegistryError(f"invalid provider slug: {provider}")
        status = entry.get("status")
        if status not in counts:
            raise RegistryError(f"invalid status for {provider}: {status}")
        counts[status] += 1
        for name in [provider, *entry.get("aliases", [])]:
            normalized = name.casefold()
            if normalized in seen_names:
                raise RegistryError(
                    f"duplicate provider name {name}: {provider} and {seen_names[normalized]}"
                )
            seen_names[normalized] = provider
        if status == "verified":
            for field in ("label", "github_login", "email", "verification"):
                if not entry.get(field):
                    raise RegistryError(f"verified provider {provider} lacks {field}")
            verification = entry["verification"]
            kind = verification.get("kind")
            if kind not in {"github_profile", "rendered_commit"}:
                raise RegistryError(f"invalid verification kind for {provider}")
            if kind == "github_profile" and not isinstance(verification.get("id"), int):
                raise RegistryError(f"invalid GitHub profile proof for {provider}")
            if kind == "rendered_commit":
                repository = verification.get("repository", "")
                parts = verification.get("commit_parts")
                if not re.fullmatch(
                    r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+",
                    repository,
                ) or not (
                    isinstance(parts, list)
                    and len(parts) == 2
                    and all(isinstance(part, str) and len(part) == 20 for part in parts)
                    and re.fullmatch(r"[0-9a-fA-F]{40}", "".join(parts))
                ):
                    raise RegistryError(f"invalid rendered commit proof for {provider}")
        else:
            for field in ("reason", "evidence", "checked_at"):
                if not entry.get(field):
                    raise RegistryError(f"unavailable provider {provider} lacks {field}")
    return counts


def rendered_authors(document: str) -> list[dict[str, Any]]:
    for match in re.finditer(r'"authors":(\[[^]]*\])', document):
        try:
            authors = json.loads(match.group(1))
        except json.JSONDecodeError:
            continue
        if isinstance(authors, list):
            return [author for author in authors if isinstance(author, dict)]
    raise RegistryError("GitHub page did not expose a rendered authors record")


def verify_profile(login: str, email: str) -> tuple[bool, dict[str, Any]]:
    match = NOREPLY_PATTERN.fullmatch(email)
    if not match:
        raise RegistryError("provider-domain emails require --proof-commit")
    quoted_login = urllib.parse.quote(login, safe="")
    profile = read_json(f"https://api.github.com/users/{quoted_login}")
    expected_id = int(match.group("id"))
    valid = (
        profile.get("login", "").casefold() == login.casefold()
        and profile.get("id") == expected_id
        and match.group("login").casefold() == login.casefold()
    )
    return valid, {"kind": "github_profile", "id": expected_id}


def compact_proof_commit(url: str) -> dict[str, Any]:
    match = GITHUB_COMMIT_PATTERN.fullmatch(url)
    if not match:
        raise RegistryError("proof commit must be a canonical GitHub commit URL with a full hash")
    commit = match.group("commit").lower()
    return {
        "kind": "rendered_commit",
        "repository": match.group("repository"),
        "commit_parts": [commit[:20], commit[20:]],
    }


def verify_proof_commit(login: str, email: str, url: str) -> tuple[bool, dict[str, Any]]:
    verification = compact_proof_commit(url)
    document = read_text(url)
    readable_document = html.unescape(document)
    trailer = re.compile(
        rf"Co-authored-by:[^\n<]*<{re.escape(email)}>",
        re.IGNORECASE,
    )
    if not trailer.search(readable_document):
        raise RegistryError("proof commit does not contain the candidate email")
    expected_login = login.casefold()
    valid = any(
        str(author.get("login", "")).casefold() == expected_login
        for author in rendered_authors(document)
    )
    return valid, verification


def command_list(args: argparse.Namespace) -> int:
    registry = load_registry(args.registry)
    validate_registry(registry)
    listing = [
        {"agent": provider, "status": entry["status"], "label": entry.get("label")}
        for provider, entry in registry["providers"].items()
    ]
    print(json.dumps(listing, indent=2, sort_keys=True))
    return 0


def command_validate(args: argparse.Namespace) -> int:
    registry = load_registry(args.registry)
    counts = validate_registry(registry)
    output = {"status": "valid", **counts, "total": sum(counts.values())}
    print(json.dumps(output, indent=2, sort_keys=True))
    return 0


def command_lookup(args: argparse.Namespace) -> int:
    registry = load_registry(args.registry)
    resolved = resolve_provider(registry, args.agent)
    if resolved is None:
        print(json.dumps({"agent": args.agent, "status": "missing"}, indent=2, sort_keys=True))
        return 3
    provider, entry = resolved
    output = {"agent": provider, **entry}
    verification = output.get("verification", {})
    if verification.get("kind") == "rendered_commit":
        output["proof_url"] = (
            f"https://github.com/{verification['repository']}/commit/"
            f"{''.join(verification['commit_parts'])}"
        )
    print(json.dumps(output, indent=2, sort_keys=True))
    return 0 if entry.get("status") == "verified" else 2


def command_verify(args: argparse.Namespace) -> int:
    if NOREPLY_PATTERN.fullmatch(args.email) and not args.proof_commit:
        valid, verification = verify_profile(args.github_login, args.email)
    elif args.proof_commit:
        valid, verification = verify_proof_commit(
            args.github_login,
            args.email,
            args.proof_commit,
        )
    else:
        raise RegistryError("cannot prove this email without --proof-commit")

    verification["verified_at"] = date.today().isoformat()
    entry = {
        args.agent: {
            "aliases": [],
            "status": "verified" if valid else "unavailable",
            "label": args.label,
            "github_login": args.github_login,
            "email": args.email,
            "verification": verification,
        }
    }
    if not valid:
        entry[args.agent]["reason"] = "GitHub did not resolve the expected account"
    print(json.dumps(entry, indent=2, sort_keys=True))
    return 0 if valid else 2


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--registry", type=Path, default=REGISTRY, help=argparse.SUPPRESS)
    commands = parser.add_subparsers(dest="command", required=True)

    list_command = commands.add_parser("list", help="list every registered agent")
    list_command.set_defaults(handler=command_list)

    validate = commands.add_parser(
        "validate",
        help="validate the full registry without network access",
    )
    validate.set_defaults(handler=command_validate)

    lookup = commands.add_parser("lookup", help="read a registered agent without network access")
    lookup.add_argument("agent")
    lookup.set_defaults(handler=command_lookup)

    verify = commands.add_parser("verify", help="verify a missing agent candidate")
    verify.add_argument("--agent", required=True)
    verify.add_argument("--label", required=True)
    verify.add_argument("--github-login", required=True)
    verify.add_argument("--email", required=True)
    verify.add_argument("--proof-commit")
    verify.set_defaults(handler=command_verify)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        return args.handler(args)
    except RegistryError as error:
        print(
            json.dumps(
                {"status": "error", "reason": str(error)},
                indent=2,
                sort_keys=True,
            )
        )
        return 1


if __name__ == "__main__":
    sys.exit(main())
