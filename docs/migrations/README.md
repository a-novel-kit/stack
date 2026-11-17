# Migration guides

One immutable guide per release that asks consumers to change something. Newest first.

- [v1.43.0](./v1.43.0.md): A CLI usage error exits 2, machine output takes `--format`, unknown flag values fail, and the daemon resolves target shorthands.
- [v1.42.0](./v1.42.0.md): The CLI drops `repo create` and `repo update`; a-novel/infra's GitHub root manages repository config.
- [v1.35.0](./v1.35.0.md): An unknown subcommand under any command group fails with exit status 1.
- [v1.34.1](./v1.34.1.md): The CLI drops `run stacks` and `run service status`; use `core stacks list` and `run ps --service`.
