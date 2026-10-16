# Dependencies

Install these tools in order. Everything is yours to do, except enrollment, which the team handles.

## Choose your system

Pick your system in any tab; every page follows. Under WSL2, pick Linux.

::: warning The a-novel CLI runs on Linux and macOS
On Windows, install everything inside [WSL2](https://learn.microsoft.com/windows/wsl/install) with the Linux tabs. The Windows tabs cover the tools alone.
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

## Access

Contributors use this whole guide except the Sysadmins section. [Get enrolled](./discord#get-enrolled) first; nothing else waits on it.

[Sysadmins](/sysadmins/) hold admin rights and the organizations' credentials, and add their own setup. Maintainers get wider repository access with the contributor setup.
