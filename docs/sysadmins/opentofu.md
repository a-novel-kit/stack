# OpenTofu

[OpenTofu](https://opentofu.org) describes production as code in [a-novel/infra](https://github.com/a-novel/infra). Merges apply it, so you run `tofu` yourself only for the maintenance the [infra runbooks](https://github.com/a-novel/infra/tree/master/docs/runbooks) describe, such as repairing state or rebuilding from nothing.

## Install

Infra accepts only the version pinned in its [`.opentofu-version`](https://github.com/a-novel/infra/blob/master/.opentofu-version). Skip to [Configure](#configure) if this prints that version:

```bash
tofu version
```

Distribution and Homebrew packages follow their own releases, so Linux and macOS use OpenTofu's installer, which takes a version. It checks the download's signature with GnuPG or cosign. On macOS, run `brew install cosign` first.

:::tabs key:shell
== Linux / macOS

```bash
PIN=https://raw.githubusercontent.com/a-novel/infra/master/.opentofu-version
VERSION=$(curl -fsSL "$PIN")
curl -fsSLo /tmp/install-opentofu.sh https://get.opentofu.org/install-opentofu.sh
sh /tmp/install-opentofu.sh --install-method standalone \
  --opentofu-version "$VERSION"
rm /tmp/install-opentofu.sh
```

== Windows

```powershell
$Pin = "https://raw.githubusercontent.com/a-novel/infra/master/.opentofu-version"
$Version = (Invoke-RestMethod $Pin).Trim()
winget install --id OpenTofu.Tofu -e --version $Version
```

:::

Rerun this step whenever the pin moves, as `tofu init` refuses any other version.

## Configure

OpenTofu reaches Google Cloud as your [Google Cloud CLI](./gcloud) account. Check from an up-to-date infra checkout, where `init` reads the foundation state:

```bash
cd ~/git-projects/a-novel/app/infra
tofu -chdir=environments/production/foundation init
```

It ends with `OpenTofu has been successfully initialized!`. If it fails on credentials instead, hand OpenTofu your account and run the check again:

```bash
gcloud auth application-default login
```
