# Dependencies

This guide sets up your machine as an A-Novel contributor. Everything is yours to do except enrollment, which the team grants. Ask for it first, on [Discord](./discord).

## Choose your system

Pick your system in any tab; every page follows. Under WSL2, pick Linux.

::: warning The a-novel CLI runs on Linux and macOS
Native Windows gets the tools only: no CLI and none of its automated setup, so you install, run and manage everything yourself. To use the CLI on Windows, install everything inside [WSL2](https://learn.microsoft.com/windows/wsl/install) with the Linux tabs.
:::

## Before you begin

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu

```bash
sudo apt update && sudo apt install -y ca-certificates curl unzip
```

== Fedora

```bash
sudo dnf install -y curl unzip
```

== openSUSE

```bash
sudo zypper install -y curl unzip
```

== Arch

```bash
sudo pacman -S --needed curl unzip which inetutils openssh
```

:::
=== macOS
Install [Homebrew](https://brew.sh):

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

=== Windows
Install PowerShell 7, and run every Windows command in it:

```powershell
winget install --id Microsoft.PowerShell -e
```

Open a new terminal after each install.
::::

Commands that edit your shell profile target zsh and bash. With fish, add the same paths with `fish_add_path`.

**Sysadmins** then follow the [sysadmin setup](/sysadmins/).
