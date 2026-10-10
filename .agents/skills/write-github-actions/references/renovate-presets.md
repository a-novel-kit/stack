# Renovate presets

Read this reference when routed here by [write-github-actions](../SKILL.md): before editing
`renovate/*.json` in `a-novel-kit/workflows` or a repo's `renovate.json`. Its rules apply to the edit.

Contents:

- [Where the configuration lives](#where-the-configuration-lives)
- [One branch carries one configuration](#one-branch-carries-one-configuration)
- [Indirect Go requirements](#indirect-go-requirements)
- [Release age](#release-age)
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
  `buf-`, `mockery-`). Never add a modfile to `gomod.managerFilePatterns` without one.
- A group spanning managers, such as a service's module, images and npm client, carries the union
  of options on the group rule: `postUpdateOptions` concatenates across rules; `goGetDirs`,
  `postUpgradeTasks` and `groupName` take the last matching rule.
- Exact group rules sit after the npm `javascript dependencies` catch-all, or it takes their npm
  members.

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
own release age. The approval workflow withholds a Renovate PR's approval while
`renovate/stability-days` is pending, because no ruleset requires that status.

A release without a timestamp cannot pass the gate: under the default `minimumReleaseAgeBehaviour:
timestamp-required` it stays pending forever. MCR images and lockfile maintenance have none.

- Keep the gate off updates that carry no timestamp, such as lockfile maintenance.
- When a container image must match a package, pin the image to the package's releases with an
  annotated workflow line and disable the image's docker lookup:

  ```yaml
  container:
    # renovate: datasource=npm depName=playwright
    image: mcr.microsoft.com/playwright:v1.64.0-noble
  ```

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
