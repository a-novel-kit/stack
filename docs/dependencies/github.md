# GitHub

GitHub hosts the code, reviews and CI. Install its terminal client, [`gh`](https://cli.github.com); the `a-novel` CLI uses it too.

You need a [GitHub account](https://github.com/signup).

## Install

Skip to [Configure](#configure) if this already prints a version:

```bash
gh --version
```

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu
The distribution's `gh` is outdated; use GitHub's repository:

```bash
sudo mkdir -p -m 755 /etc/apt/keyrings
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null
sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
sudo apt update && sudo apt install -y gh
```

== Fedora

```bash
sudo dnf install -y gh
```

== openSUSE

```bash
sudo zypper install -y gh
```

== Arch

```bash
sudo pacman -S --needed github-cli
```

:::
=== macOS

```bash
brew install gh
```

=== Windows

```powershell
winget install --id GitHub.cli -e
```

::::

## Configure

### Sign in

Skip if `gh auth status` shows you logged in.

```bash
gh auth login --hostname github.com --git-protocol ssh --web --skip-ssh-key
```

Approve the one-time code in your browser.

### Create an SSH key

Git reaches GitHub over SSH, with a key registered on your account.

Skip if `ls ~/.ssh/*.pub` lists a key already, and put its path in place of `~/.ssh/id_ed25519.pub` below.

:::tabs key:shell
== Linux / macOS

```bash
ssh-keygen -t ed25519 -C "$(git config user.email)" -f ~/.ssh/id_ed25519
```

== Windows

```powershell
ssh-keygen -t ed25519 -C (git config user.email) -f "$HOME\.ssh\id_ed25519"
```

:::

### Register the key on GitHub

Skip if this greets you by name. The exit status 1 that follows is normal.

```bash
ssh -T git@github.com
```

Otherwise, register the key for authentication. `gh` asks for a temporary permission to upload it.

:::tabs key:shell
== Linux / macOS

```bash
gh auth refresh -h github.com -s admin:public_key
gh ssh-key add ~/.ssh/id_ed25519.pub --type authentication --title "$(hostname)"
gh auth refresh -h github.com --remove-scopes admin:public_key
```

== Windows

```powershell
gh auth refresh -h github.com -s admin:public_key
gh ssh-key add "$HOME\.ssh\id_ed25519.pub" --type authentication --title $env:COMPUTERNAME
gh auth refresh -h github.com --remove-scopes admin:public_key
```

:::

Run the check again: GitHub now greets you by name.

::: tip Sysadmins
Sysadmins use a stricter setup: see [GitHub with a security key](/sysadmins/#github-with-a-security-key).
:::

### Share one connection

The CLI already reuses one GitHub connection across its multi-repository commands. Let every Git command reuse it too, so pushes and pulls skip the handshake.

Skip if `ssh -G git@github.com | grep '^controlpath'` prints a path. Otherwise, on Linux or macOS:

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
cat >> ~/.ssh/config <<'EOF'

Host github.com
    ControlMaster auto
    ControlPath ~/.ssh/cm-%C
    ControlPersist 1h
EOF
```

The connection closes after an idle hour, or at once with `ssh -O exit git@github.com`. Windows OpenSSH cannot share connections.

## Accept your invitations

Once you are [enrolled](./discord#get-enrolled), GitHub emails an invitation per organization. Accept both, from the email or the invitation pages ([`a-novel`](https://github.com/orgs/a-novel/invitation), [`a-novel-kit`](https://github.com/orgs/a-novel-kit/invitation)).
