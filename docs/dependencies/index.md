# Dependencies

Install these tools in order; each page builds on the previous ones. Everything here is yours to do, except your enrollment, which the team handles.

| Tool                        | What it does here                                          |
| --------------------------- | ---------------------------------------------------------- |
| [Discord](./discord)        | Where the team syncs and talks, and where you get enrolled |
| [Git](./git)                | Versions every repository                                  |
| [GitHub](./github)          | Hosts the code, reviews and CI                             |
| [Commit signing](./signing) | Proves your commits are yours                              |
| [Go](./go)                  | Builds the services, libraries and CLI                     |
| [Node.js and pnpm](./node)  | Run the JavaScript tooling and frontends                   |
| [Podman](./podman)          | Runs databases and services in containers                  |

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

## Check what you already have

A tool that passes its check only needs the Configure step of its page.

| Tool       | Check                                        | Needed                                       |
| ---------- | -------------------------------------------- | -------------------------------------------- |
| Git        | `git --version`                              | Any current release                          |
| GitHub CLI | `gh --version`                               | Any current release                          |
| Go         | `go version`                                 | The version in the stack's `cli/go.mod`      |
| Node.js    | `node --version`                             | `engines.node` in the stack's `package.json` |
| pnpm       | `pnpm --version`                             | Any current release                          |
| Podman     | `podman --version`, `podman compose version` | podman-compose 1.6.0 or newer                |

Print the current Go and Node.js minimums:

```bash
curl -fsSL https://raw.githubusercontent.com/a-novel-kit/stack/master/cli/go.mod | grep '^go '
curl -fsSL https://raw.githubusercontent.com/a-novel-kit/stack/master/package.json | grep '"node"'
```

## Access

Access comes in two levels:

- **Contributors** get the member role on Discord, which opens the private working channels, and access to the GitHub repositories they work on. [Get enrolled](./discord#get-enrolled) first: the rest of these pages needs no access, so install while you wait.
- **Sysadmins** hold admin rights and the organizations' credentials, with [extra requirements](/sysadmins/).

Maintainers get wider repository access than contributors, with the same setup.
