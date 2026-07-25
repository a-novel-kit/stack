#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const defaultRegistry = resolve(scriptDirectory, "../references/providers.json");
const providerPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const repositoryPattern = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;
const commitPattern = /^[0-9a-fA-F]{40}$/;

class RegistryError extends Error {}

function output(value) {
  process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
}

function loadRegistry(path) {
  let registry;
  try {
    registry = JSON.parse(readFileSync(path, "utf8"));
  } catch (error) {
    throw new RegistryError(`could not read registry ${path}: ${error.message}`);
  }
  if (
    registry.schema !== 1 ||
    !registry.providers ||
    typeof registry.providers !== "object" ||
    Array.isArray(registry.providers)
  ) {
    throw new RegistryError(`unsupported registry schema in ${path}`);
  }
  if (!registry.scope || !registry.as_of) {
    throw new RegistryError(`registry scope or as_of date missing in ${path}`);
  }
  return registry;
}

function resolveProvider(registry, requested) {
  const normalized = requested.toLocaleLowerCase();
  for (const [provider, entry] of Object.entries(registry.providers)) {
    const names = [provider, ...(entry.aliases ?? [])].map((name) => name.toLocaleLowerCase());
    if (names.includes(normalized)) return [provider, entry];
  }
  return null;
}

function validateRegistry(registry) {
  const seenNames = new Map();
  const counts = { verified: 0, unavailable: 0 };
  for (const [provider, entry] of Object.entries(registry.providers)) {
    if (!providerPattern.test(provider)) throw new RegistryError(`invalid provider slug: ${provider}`);
    if (!Object.hasOwn(counts, entry.status)) {
      throw new RegistryError(`invalid status for ${provider}: ${entry.status}`);
    }
    counts[entry.status] += 1;
    for (const name of [provider, ...(entry.aliases ?? [])]) {
      const normalized = name.toLocaleLowerCase();
      if (seenNames.has(normalized)) {
        throw new RegistryError(`duplicate provider name ${name}: ${provider} and ${seenNames.get(normalized)}`);
      }
      seenNames.set(normalized, provider);
    }
    if (entry.status === "verified") {
      for (const field of ["label", "github_login", "email", "verification"]) {
        if (!entry[field]) throw new RegistryError(`verified provider ${provider} lacks ${field}`);
      }
      const verification = entry.verification;
      if (!["github_profile", "rendered_commit"].includes(verification.kind)) {
        throw new RegistryError(`invalid verification kind for ${provider}`);
      }
      if (verification.kind === "github_profile" && !Number.isInteger(verification.id)) {
        throw new RegistryError(`invalid GitHub profile proof for ${provider}`);
      }
      if (verification.kind === "rendered_commit") {
        const parts = verification.commit_parts;
        const validParts =
          Array.isArray(parts) &&
          parts.length === 2 &&
          parts.every((part) => typeof part === "string" && part.length === 20) &&
          commitPattern.test(parts.join(""));
        if (!repositoryPattern.test(verification.repository ?? "") || !validParts) {
          throw new RegistryError(`invalid rendered commit proof for ${provider}`);
        }
      }
    } else {
      for (const field of ["reason", "evidence", "checked_at"]) {
        if (!entry[field]) throw new RegistryError(`unavailable provider ${provider} lacks ${field}`);
      }
    }
  }
  return counts;
}

function parseArguments(arguments_) {
  const values = [...arguments_];
  let registryPath = defaultRegistry;
  if (values[0] === "--registry") {
    if (!values[1]) throw new RegistryError("--registry requires a path");
    registryPath = resolve(values[1]);
    values.splice(0, 2);
  }
  return { registryPath, command: values[0], operands: values.slice(1) };
}

function main() {
  const { registryPath, command, operands } = parseArguments(process.argv.slice(2));
  const registry = loadRegistry(registryPath);
  const counts = validateRegistry(registry);
  if (command === "list" && operands.length === 0) {
    output(
      Object.entries(registry.providers).map(([agent, entry]) => ({
        agent,
        status: entry.status,
        label: entry.label,
      }))
    );
    return 0;
  }
  if (command === "validate" && operands.length === 0) {
    output({ status: "valid", ...counts, total: counts.verified + counts.unavailable });
    return 0;
  }
  if (command === "lookup" && operands.length === 1) {
    const resolvedProvider = resolveProvider(registry, operands[0]);
    if (!resolvedProvider) {
      output({ agent: operands[0], status: "missing" });
      return 3;
    }
    const [agent, entry] = resolvedProvider;
    const result = { agent, ...entry };
    if (entry.verification?.kind === "rendered_commit") {
      result.proof_url = `https://github.com/${entry.verification.repository}/commit/${entry.verification.commit_parts.join("")}`;
    }
    output(result);
    return entry.status === "verified" ? 0 : 2;
  }
  throw new RegistryError("usage: coauthor-registry.mjs [--registry <path>] list|validate|lookup <agent>");
}

try {
  process.exitCode = main();
} catch (error) {
  output({ status: "error", reason: error instanceof Error ? error.message : String(error) });
  process.exitCode = 1;
}
