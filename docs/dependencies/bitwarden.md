# Bitwarden (maintainers)

::: info Maintainers only
Only maintainers who apply repository settings with `a-novel repo update` need `bws`. Developers skip this page.
:::

[Bitwarden Secrets Manager](https://bitwarden.com/products/secrets-manager/) holds the organizations' shared credentials. Maintainers read them with `bws`, its CLI, when `a-novel repo update` provisions them into the repositories.

## Install

Skip to [Configure](#configure) if this already prints a version:

```bash
bws --version
```

:::tabs key:os
== Linux
Bitwarden's installer verifies the download before installing it:

```bash
curl -fsSL https://bws.bitwarden.com/install | sh
```

== macOS
Bitwarden's installer verifies the download before installing it:

```bash
curl -fsSL https://bws.bitwarden.com/install | sh
```

== Windows

```powershell
winget install --id Bitwarden.BWS -e
```

:::

## Configure

`bws` authenticates with the access token of a machine account. Ask an owner for:

- an access token for the organizations' credentials;
- the Bitwarden region the organization uses, US or EU cloud.

On the EU cloud, point `bws` at it:

```bash
bws config server-base https://vault.bitwarden.eu
```

Check the token. It is read with no echo and kept only for this session, so it lands in neither the terminal nor your history:

:::tabs key:shell
== Linux / macOS

```bash
read -rs BWS_ACCESS_TOKEN   # paste the token, then press Enter
export BWS_ACCESS_TOKEN
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

The list shows the projects the token reads. Once the CLI is installed, store the token in its encrypted store, as [Configure repositories](/cli/repositories#store-the-bitwarden-token) shows.
