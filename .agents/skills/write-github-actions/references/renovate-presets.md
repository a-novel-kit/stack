# Renovate presets

Read this reference when routed here by [write-github-actions](../SKILL.md): before editing
`renovate/*.json` in `a-novel-kit/workflows` or a repo's `renovate.json`. Its rules apply to the edit.

Contents:

- [Where the configuration lives](#where-the-configuration-lives)
- [One branch carries one configuration](#one-branch-carries-one-configuration)
- [Indirect Go requirements](#indirect-go-requirements)
- [Release age](#release-age)
- [Release files pinned by hash](#release-files-pinned-by-hash)
- [Proving a rule](#proving-a-rule)

## Where the configuration lives

Each repo's `renovate.json` extends one class preset (`renovate/<class>.json`) pinned to a workflows
release; every class extends `renovate/base.json`. `docs/renovate-presets.md` in the workflows repo
documents the presets for consumers. A preset change reaches a repo in two Renovate runs: one
proposes the bump of the pinned release, the next regenerates branches under the new preset.

The workflows release stamps every self-reference, including its own `renovate.json`
(`github>a-novel-kit/workflows//renovate/workflows#vX.Y.Z`). A Renovate PR bumping the workflows
repo inside the workflows repo means a self-pin escaped the stamp.

Holds use `allowedVersions` ranges that exclude one bad release and say when to drop the rule.
`node-actions/audit` owns pnpm overrides, so Renovate is disabled for the `pnpm.overrides`,
`overrides` and `pnpm-workspace.overrides` dep types. A security PR still updates an override whose
value is itself vulnerable, to the minimal fixed version.

## One branch carries one configuration

Renovate runs a branch's artifact updates with the configuration of its first upgrade by `depName`.
`matchFileNames` does not scope `goGetDirs`, `postUpgradeTasks` or `postUpdateOptions` within a
branch, so a modfile sharing a branch can impose its options on another and silently drop `go mod
tidy`.

- Give every Go tool modfile its own `additionalBranchPrefix` (`golangci-lint-`, `gotestsum-`,
  `govulncheck-`, `buf-`, `mockery-`). Never add a modfile to `gomod.managerFilePatterns` without
  one; `tests/renovate.test.mjs` fails when one is missing.
- A group spanning managers, such as a service's module, images and npm client, carries the union
  of options on the group rule: `postUpdateOptions` concatenates across rules; `goGetDirs`,
  `postUpgradeTasks` and `groupName` take the last matching rule.
- Exact group rules sit after the npm `javascript dependencies` catch-all, or it takes their npm
  members.
- A group never waits for a member that is not yet published. When one version must exist in two
  registries, as the `go` directive must exist as a golang alpine image, have one dependency read the
  other's releases with `overrideDatasource`, `overridePackageName` and `extractVersion`. A release
  age only guesses the lag, and the group splits whenever the other member arrives first.

## Indirect Go requirements

Renovate's gomod manager disables `// indirect` requires. Re-enable one only where staying behind
breaks something:

- The Bun group enables its dialects, which must match the core version.
- `golang.org/x/tools` is enabled in `golangci-lint.mod` and `mockery.mod`. Type-checking tools read
  the compiler's export data through it, and a Go patch release can raise that format.
- `osvVulnerabilityAlerts` with `vulnerabilityAlerts.enabled` opens a `[security]` PR for a
  vulnerable indirect module, and only for that one. It opens one PR per module per modfile.

## Release age

`minimumReleaseAge: "3 days"` applies to `matchDatasources: ["npm"]` and stays at or above pnpm's
own release age. Third-party GitHub Actions wait the same three days
(`matchManagers: ["github-actions"]`), and first-party releases skip both gates: the release proof
the auto-approver checks stands in for the wait. The approval workflow withholds a Renovate PR's
approval while `renovate/stability-days` is pending, because no ruleset requires that status.

Compose files and Dockerfiles pin first-party images by digest (`pinDigests` for those two managers),
so a moved tag changes nothing a consumer runs and each bump names the digest the approver checks.
The composite-action default manager keeps a digest written after the tag, and a default gets that
digest by hand once.

A release without a timestamp cannot pass the gate: under the default `minimumReleaseAgeBehaviour:
timestamp-required` it stays pending forever. MCR images and lockfile maintenance have none.

- Keep the gate off updates that carry no timestamp, such as lockfile maintenance.
- Lockfile maintenance obeys only pnpm's shorter release age, so it moves a range past the version
  the group PR still offers. Inside one lockstep group, such as `vitest monorepo`, every repo uses
  one specifier form: all exact, or all the same range. A preset `rangeStrategy: "pin"` cannot
  enforce it: its pin update and the group's own update collide in one branch, the pin wins, and
  the group PR splits the family the other way.
- When a container image must match a package, pin the image to the package's releases with an
  annotated workflow line and disable the image's docker lookup:

  ```yaml
  container:
    # renovate: datasource=npm depName=playwright
    image: mcr.microsoft.com/playwright:v1.64.0-noble
  ```

## Release files pinned by hash

A workflow pin of a downloaded tool carries the SHA-256 of the file its action downloads, on the
line after `version`. The base preset's workflow manager reads it as the digest:

```yaml
# renovate: datasource=custom.hadolint depName=hadolint/hadolint
version: "2.15.1"
sha256: "c7187db94eeeeca956519a6af171adc31453941a1e777961f6e680f697c8c507"
```

- Each tool has a custom datasource in `renovate/base.json` that reads its file's `digest` from
  GitHub's releases API and lists only the releases that carry one. A new tool adds its own, modeled
  on `shellcheck` and `hadolint`, and joins the one-hour `github-releases` delay, which
  `tests/renovate.test.mjs` checks.
- A digest update keeps the version and changes the bytes, so someone replaced the release's files.
  The digest review rule matches `custom.regex` as well as `github-actions`, so it waits for a
  reviewer, as do the database preset's pgBackRest and apko hashes.
- Prove a new datasource with a scratch repo pinned one release back, as in
  [Proving a rule](#proving-a-rule): the lookup moves the version and the digest together.

## Proving a rule

A green Renovate PR does not prove a rule, because branch order changes the outcome. Run the pinned
Renovate CLI against a scratch repo holding the relevant manifests, with the preset as its
`renovate.json`:

```bash
LOG_LEVEL=debug LOG_FORMAT=json GITHUB_COM_TOKEN="$(gh auth token)" \
  renovate --platform=local --dry-run=lookup > renovate.log
jq -r 'select(.msg | test("flattened updates")) | .msg' renovate.log
```

Compare against a run without the rule. The `packageFiles with updates` entry shows each dependency's
`updates`, `pendingChecks` and `branchName`.
