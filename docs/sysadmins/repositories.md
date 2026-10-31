# Configure repositories

Repository settings are code. The [GitHub root](https://github.com/a-novel/infra/tree/master/environments/production/github) in a-novel/infra holds every repository of both organizations, with its settings, team access, labels, rulesets, environments and secret access. Its next apply reverts a manual change, so change settings through it.

## Change a setting

Edit the repository's entry under `repositories` in `environments/production/github/terraform.tfvars`, or its class in `main.tf` to change every repository of that class. Open a pull request: its `plan-github` check shows the change, and the merge's deploy applies it.

## Follow the code

Required checks, environments and secret access follow each repository's workflows on its default branch, so nothing needs running. Within 10 minutes of a merge that changes them, the root's sweep applies the new set, as infra's [Repository config](https://github.com/a-novel/infra/blob/master/docs/architecture.md#repository-config) describes.
