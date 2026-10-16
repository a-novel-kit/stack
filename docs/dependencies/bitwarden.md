# Bitwarden (maintainers)

::: info Maintainers only
Developers skip this page.
:::

[Bitwarden Secrets Manager](https://bitwarden.com/products/secrets-manager/) holds the organizations' privileged keys, one project per GitHub organization, along with the map CI uses to mention people on Discord. Maintainers read the keys with `bws`, the Secrets Manager CLI, when `a-novel repo update` provisions them into the repositories.

## Install

:::tabs key:os
== Linux
Bitwarden's installer checks the download against its published checksum, then installs to `/usr/local/bin`, or `~/.local/bin` without sudo:

```bash
curl -fsSL https://bws.bitwarden.com/install | sh
```

== macOS
Bitwarden's installer checks the download against its published checksum, then installs to `/usr/local/bin`, or `~/.local/bin` without sudo:

```bash
curl -fsSL https://bws.bitwarden.com/install | sh
```

== Windows

```powershell
winget install --id Bitwarden.BWS -e
```

:::

```bash
bws --version
```

## Configure

`bws` authenticates with the access token of a machine account. Ask an owner for:

- an access token for the machine account that reads both organizations' projects;
- the Bitwarden region the organization uses, US or EU cloud.

On the EU cloud, point `bws` at it:

```bash
bws config server-base https://vault.bitwarden.eu
```

Check the token. It is read with no echo and kept only for this session, so it lands in neither the terminal nor your history:

:::tabs key:shell
== Linux / macOS

```bash
read -rs BWS_ACCESS_TOKEN && export BWS_ACCESS_TOKEN   # paste the token, then press Enter
bws project list
unset BWS_ACCESS_TOKEN
```

== Windows

```powershell
$env:BWS_ACCESS_TOKEN = Read-Host -MaskInput "Access token"
bws project list
Remove-Item Env:BWS_ACCESS_TOKEN
```

:::

The list shows one project per organization. Once the CLI is installed, store the token in its encrypted store, as [Configure repositories](/cli/repositories#store-the-bitwarden-token) shows.
