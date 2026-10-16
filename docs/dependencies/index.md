# Dependencies

This section sets up the tools A-Novel development relies on, one page each. Work through them in order, since later pages build on earlier ones: the GitHub page uses the identity you give Git, and the signing page uses the key you create for GitHub.

A tool's page has two parts. **Install** opens with a check: if the tool is already there and recent enough, jump to **Configure**, which holds the A-Novel-specific setup everyone needs. Access granted by the team takes time, so read [what to ask the team for](#what-to-ask-the-team-for) before you start.

| Tool                        | What it does here                                        | Who needs it |
| --------------------------- | -------------------------------------------------------- | ------------ |
| [Git](./git)                | Versions every repository                                | Everyone     |
| [GitHub](./github)          | Hosts the code, reviews and CI; `gh` drives it           | Everyone     |
| [Commit signing](./signing) | Proves your commits come from you                        | Everyone     |
| [Go](./go)                  | Builds the services, the libraries and the `a-novel` CLI | Everyone     |
| [Node.js and pnpm](./node)  | Run the JavaScript tooling and the frontend platforms    | Everyone     |
| [Podman](./podman)          | Runs databases and services in containers                | Everyone     |
| [Discord](./discord)        | Team chat, and where CI mentions you                     | Everyone     |
| [Bitwarden](./bitwarden)    | Holds the organizations' privileged keys                 | Maintainers  |
| [AI agents](./ai-agents)    | Coding agents preloaded with the team's conventions      | Optional     |

## Choose your system

Commands come in tabs. Pick your system once: every page follows, and the browser remembers it. Linux adds a second row of tabs, one per distribution family.

| Tab     | Covers                                                                           |
| ------- | -------------------------------------------------------------------------------- |
| Linux   | Debian and Ubuntu, Fedora, openSUSE, Arch. WSL2 on Windows runs Linux: use these |
| macOS   | Installs through Homebrew                                                        |
| Windows | Native Windows, through winget and PowerShell 7                                  |

::: warning The a-novel CLI runs on Linux and macOS
The CLI supervises processes the Unix way, so it does not run natively on Windows. To use it from a Windows machine, install the dependencies inside [WSL2](https://learn.microsoft.com/windows/wsl/install) with the Linux tabs. The Windows tabs set up the same tools for work outside the CLI.
:::

## Before you begin

Install the base tools the other pages assume.

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
Install [Homebrew](https://brew.sh), the package manager every macOS tab uses:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

=== Windows
winget ships with Windows 11. Install PowerShell 7, then run every Windows command in it:

```powershell
winget install --id Microsoft.PowerShell -e
```

Open a new terminal after each winget install, so the new command is on your `PATH`.
::::

Some Linux and macOS commands add a line to your shell profile. They target `$HOME/.$(basename "$SHELL")rc`, which is `~/.zshrc` under zsh and `~/.bashrc` under bash. Under fish, add the same directories with `fish_add_path`.

## Check what you already have

Run each check; a tool that answers with a recent enough version needs only its page's Configure section.

| Tool       | Check                                        | Needed                                       |
| ---------- | -------------------------------------------- | -------------------------------------------- |
| Git        | `git --version`                              | Any current release                          |
| GitHub CLI | `gh --version`                               | Any current release                          |
| Go         | `go version`                                 | The version in the stack's `cli/go.mod`      |
| Node.js    | `node --version`                             | `engines.node` in the stack's `package.json` |
| pnpm       | `pnpm --version`                             | Any current release                          |
| Podman     | `podman --version`, `podman compose version` | podman-compose 1.6.0 or newer                |
| Bitwarden  | `bws --version`                              | Any current release, for maintainers         |

The Go and Node.js minimums move with the stack. Print the current ones:

```bash
curl -fsSL https://raw.githubusercontent.com/a-novel-kit/stack/master/cli/go.mod | grep '^go '
curl -fsSL https://raw.githubusercontent.com/a-novel-kit/stack/master/package.json | grep '"node"'
```

## What to ask the team for

Some access is granted by the team rather than set up by you. [Join Discord](./discord#join) first and ask there as soon as you have the account a row names: you can keep installing while a request is pending, and the linked page says what to send.

| Access                                                 | Ask          | Page                                                   |
| ------------------------------------------------------ | ------------ | ------------------------------------------------------ |
| Membership in both GitHub organizations and area teams | A maintainer | [GitHub](./github#request-access)                      |
| Discord mentions for your accounts and repositories    | A maintainer | [Discord](./discord#get-mentioned)                     |
| Comments as the organization bot, for agent workflows  | A maintainer | [Comment as the bot](/cli/bot-comments#request-access) |
| A Secrets Manager access token, for maintainers        | An owner     | [Bitwarden](./bitwarden#configure)                     |
