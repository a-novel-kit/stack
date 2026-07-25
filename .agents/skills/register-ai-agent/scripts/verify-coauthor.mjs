#!/usr/bin/env node

const userAgent = "a-novel-ai-coauthor-verifier/1";
const noreplyPattern = /^(?<id>[0-9]+)\+(?<login>.+)@users\.noreply\.github\.com$/i;
const commitUrlPattern =
  /^https:\/\/github\.com\/(?<repository>[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)\/commit\/(?<commit>[0-9a-fA-F]{40})\/?$/;

class VerificationError extends Error {}

function output(value) {
  process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
}

async function read(url, accept) {
  let response;
  try {
    response = await fetch(url, {
      headers: { Accept: accept, "User-Agent": userAgent },
      signal: AbortSignal.timeout(20_000),
    });
  } catch (error) {
    throw new VerificationError(`could not read ${url}: ${error.message}`);
  }
  if (!response.ok) throw new VerificationError(`could not read ${url}: HTTP ${response.status}`);
  return response;
}

function compactProofCommit(url) {
  const match = commitUrlPattern.exec(url);
  if (!match?.groups) {
    throw new VerificationError("proof commit must be a canonical GitHub commit URL with a full hash");
  }
  const commit = match.groups.commit.toLocaleLowerCase();
  return {
    kind: "rendered_commit",
    repository: match.groups.repository,
    commit_parts: [commit.slice(0, 20), commit.slice(20)],
  };
}

function decodeHtml(value) {
  return value
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">")
    .replaceAll("&quot;", '"')
    .replaceAll("&#39;", "'")
    .replaceAll("&amp;", "&");
}

function renderedAuthors(document) {
  const matches = document.matchAll(/"authors":(\[[^\]]*\])/g);
  for (const match of matches) {
    try {
      const authors = JSON.parse(match[1]);
      if (Array.isArray(authors)) return authors.filter((author) => author && typeof author === "object");
    } catch {
      // Continue until a valid rendered authors record is found.
    }
  }
  throw new VerificationError("GitHub page did not expose a rendered authors record");
}

async function verifyProfile(login, email) {
  const match = noreplyPattern.exec(email);
  if (!match?.groups) throw new VerificationError("provider-domain emails require --proof-commit");
  const response = await read(
    `https://api.github.com/users/${encodeURIComponent(login)}`,
    "application/vnd.github+json"
  );
  const profile = await response.json();
  const expectedId = Number(match.groups.id);
  const valid =
    profile.login?.toLocaleLowerCase() === login.toLocaleLowerCase() &&
    profile.id === expectedId &&
    match.groups.login.toLocaleLowerCase() === login.toLocaleLowerCase();
  return [valid, { kind: "github_profile", id: expectedId }];
}

async function verifyProofCommit(login, email, url) {
  const verification = compactProofCommit(url);
  const response = await read(url, "text/html");
  const document = await response.text();
  const readableDocument = decodeHtml(document);
  const escapedEmail = email.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const trailer = new RegExp(`Co-authored-by:[^\\n<]*<${escapedEmail}>`, "i");
  if (!trailer.test(readableDocument)) {
    throw new VerificationError("proof commit does not contain the candidate email");
  }
  const valid = renderedAuthors(document).some(
    (author) => String(author.login ?? "").toLocaleLowerCase() === login.toLocaleLowerCase()
  );
  return [valid, verification];
}

function parseOptions(arguments_) {
  const options = {};
  for (let index = 0; index < arguments_.length; index += 2) {
    const key = arguments_[index];
    const value = arguments_[index + 1];
    if (!key?.startsWith("--") || value === undefined) {
      throw new VerificationError("options must use --name <value> pairs");
    }
    options[key.slice(2).replaceAll("-", "_")] = value;
  }
  for (const required of ["agent", "label", "github_login", "email"]) {
    if (!options[required]) throw new VerificationError(`--${required.replaceAll("_", "-")} is required`);
  }
  return options;
}

async function main() {
  const options = parseOptions(process.argv.slice(2));
  let valid;
  let verification;
  if (noreplyPattern.test(options.email) && !options.proof_commit) {
    [valid, verification] = await verifyProfile(options.github_login, options.email);
  } else if (options.proof_commit) {
    [valid, verification] = await verifyProofCommit(options.github_login, options.email, options.proof_commit);
  } else {
    throw new VerificationError("cannot prove this email without --proof-commit");
  }
  verification.verified_at = new Date().toISOString().slice(0, 10);
  const candidate = {
    aliases: [],
    status: valid ? "verified" : "unavailable",
    label: options.label,
    github_login: options.github_login,
    email: options.email,
    verification,
  };
  if (!valid) candidate.reason = "GitHub did not resolve the expected account";
  output({ [options.agent]: candidate });
  return valid ? 0 : 2;
}

try {
  process.exitCode = await main();
} catch (error) {
  output({ status: "error", reason: error instanceof Error ? error.message : String(error) });
  process.exitCode = 1;
}
