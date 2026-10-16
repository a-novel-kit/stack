# Bitwarden

[Bitwarden Secrets Manager](https://bitwarden.com/products/secrets-manager/) holds the organizations' shared credentials. `a-novel repo update` reads them through `bws`, its CLI.

## Install

Skip to [Configure](#configure) if this prints a version:

```bash
bws --version
```

:::tabs key:os
== Linux

```bash
curl -fsSL https://bws.bitwarden.com/install | sh
```

== macOS

```bash
curl -fsSL https://bws.bitwarden.com/install | sh
```

== Windows

```powershell
winget install --id Bitwarden.BWS -e
```

:::

## Configure

Ask another sysadmin for an access token, and whether the organization uses Bitwarden's US or EU cloud. On the EU cloud:

```bash
bws config server-base https://vault.bitwarden.eu
```

Check the token:

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

Once the CLI is installed, [store the token](./repositories#store-the-bitwarden-token) in its encrypted store.
