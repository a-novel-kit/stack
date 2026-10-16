# GitHub

GitHub hosts the code, reviews and CI. The [GitHub CLI](https://cli.github.com), `gh`, works with it from the terminal, and the `a-novel` CLI relies on it.

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

### Add an SSH key

Git talks to GitHub over SSH, and the same key [signs your commits](./signing). Prefer a hardware security key, such as a YubiKey: the private key never leaves it. A key file works without hardware.

Already have a key on GitHub? If `ssh -T git@github.com` greets you by name, skip to [Share one connection](#share-one-connection).

`gh` asks for a temporary permission to upload the key.

::::tabs key:sshkey
=== Security key
Plug the key in. On macOS, run `brew install openssh` first: the system OpenSSH lacks security key support.

:::tabs key:shell
== Linux / macOS

```bash
ssh-keygen -t ed25519-sk -C "$(git config user.email)" -f ~/.ssh/id_ed25519_sk
gh auth refresh -h github.com -s admin:public_key
gh ssh-key add ~/.ssh/id_ed25519_sk.pub --type authentication --title "$(hostname)"
gh auth refresh -h github.com --remove-scopes admin:public_key
```

== Windows
The first command makes Git use Windows OpenSSH, which reaches the key through Windows Hello.

```powershell
git config --global core.sshCommand "C:/Windows/System32/OpenSSH/ssh.exe"
ssh-keygen -t ed25519-sk -C (git config user.email) -f "$HOME\.ssh\id_ed25519_sk"
gh auth refresh -h github.com -s admin:public_key
gh ssh-key add "$HOME\.ssh\id_ed25519_sk.pub" --type authentication --title $env:COMPUTERNAME
gh auth refresh -h github.com --remove-scopes admin:public_key
```

:::

Register a backup key the same way, with `-f ~/.ssh/id_ed25519_sk_backup`, and list both keys as `IdentityFile` under `Host github.com` in `~/.ssh/config`.
=== Key file
Set a passphrase when asked.

:::tabs key:shell
== Linux / macOS

```bash
ssh-keygen -t ed25519 -C "$(git config user.email)" -f ~/.ssh/id_ed25519
gh auth refresh -h github.com -s admin:public_key
gh ssh-key add ~/.ssh/id_ed25519.pub --type authentication --title "$(hostname)"
gh auth refresh -h github.com --remove-scopes admin:public_key
```

== Windows

```powershell
ssh-keygen -t ed25519 -C (git config user.email) -f "$HOME\.ssh\id_ed25519"
gh auth refresh -h github.com -s admin:public_key
gh ssh-key add "$HOME\.ssh\id_ed25519.pub" --type authentication --title $env:COMPUTERNAME
gh auth refresh -h github.com --remove-scopes admin:public_key
```

:::
::::

Check the connection:

```bash
ssh -T git@github.com
```

GitHub greets you by name; the exit status 1 is normal.

### Share one connection

Each new SSH connection authenticates again: one more touch on a security key. Let SSH keep one connection open and reuse it for every push, fetch and clone; `a-novel core sync` reuses it too.

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

The connection closes after an idle hour. While it is open, any program running as you can use it, so close it when you step away:

```bash
ssh -O check git@github.com   # is a connection open?
ssh -O exit git@github.com    # close it now
```

Windows OpenSSH cannot share connections; WSL2 can.

## Accept your invitations

Once you are [enrolled](./discord#get-enrolled), GitHub emails an invitation per organization. Accept both, from the email or the invitation pages ([`a-novel`](https://github.com/orgs/a-novel/invitation), [`a-novel-kit`](https://github.com/orgs/a-novel-kit/invitation)).
