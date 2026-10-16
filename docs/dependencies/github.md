# GitHub

GitHub hosts the code, the reviews and the CI of both A-Novel organizations. The [GitHub CLI](https://cli.github.com), `gh`, drives pull requests and the GitHub API from the terminal, and the `a-novel` CLI calls it too.

You need a GitHub account: [sign up](https://github.com/signup) if you have none.

## Install

Skip to [Configure](#configure) if this already prints a version:

```bash
gh --version
```

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu
The distribution's `gh` is years behind; install it from GitHub's own repository:

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

Skip this step if `gh auth status` already shows you logged in to github.com.

```bash
gh auth login --hostname github.com --git-protocol ssh --web --skip-ssh-key
```

`gh` prints a one-time code and opens your browser to approve it. It skips the SSH key offer because the next step creates the key. Check the session:

```bash
gh auth status
```

### Add an SSH key

Git reaches GitHub over SSH, and the same key [signs your commits](./signing). A hardware security key, such as a YubiKey, keeps the private key on the device: nothing authenticates without a touch. A key file needs no hardware.

Skip to [Share one connection](#share-one-connection) if your GitHub account already has a key for this machine, which `ssh -T git@github.com` confirms by greeting you by name. The [signing page](./signing) then needs that key's path in place of the one its commands use.

`gh` asks for one extra permission to upload the key, through the browser again, and the last command gives it back.

::::tabs key:sshkey
=== Security key
Plug the key in. On macOS, first run `brew install openssh` and open a new terminal: the system OpenSSH has no security key support.

:::tabs key:shell
== Linux / macOS

```bash
ssh-keygen -t ed25519-sk -C "$(git config user.email)" -f ~/.ssh/id_ed25519_sk
gh auth refresh -h github.com -s admin:public_key
gh ssh-key add ~/.ssh/id_ed25519_sk.pub --type authentication --title "$(hostname)"
gh auth refresh -h github.com --remove-scopes admin:public_key
```

== Windows
Windows OpenSSH reaches the key through Windows Hello; the first command points Git at it rather than the client Git bundles.

```powershell
git config --global core.sshCommand "C:/Windows/System32/OpenSSH/ssh.exe"
ssh-keygen -t ed25519-sk -C (git config user.email) -f "$HOME\.ssh\id_ed25519_sk"
gh auth refresh -h github.com -s admin:public_key
gh ssh-key add "$HOME\.ssh\id_ed25519_sk.pub" --type authentication --title $env:COMPUTERNAME
gh auth refresh -h github.com --remove-scopes admin:public_key
```

:::

Register a second security key as a backup, so losing one does not lock you out: repeat with `-f ~/.ssh/id_ed25519_sk_backup`, then list both keys as `IdentityFile` lines under `Host github.com` in `~/.ssh/config`.
=== Key file
Pick a passphrase when asked: it protects the file if the disk is ever copied.

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

GitHub answers `Hi <you>! You've successfully authenticated, but GitHub does not provide shell access.` and the command exits with status 1, which is expected.

### Share one connection

Every new SSH connection to GitHub authenticates again, which with a security key means another touch. SSH can instead keep one authenticated connection open for an hour and carry every push, fetch and clone through it, and `a-novel core sync` reuses that connection for all repositories rather than opening one each. It helps with any key, and spares a security key user most of the touches.

Linux and macOS share connections from a block in your SSH config:

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
cat >> ~/.ssh/config <<'EOF'

Host github.com
    ControlMaster auto
    ControlPath ~/.ssh/cm-%C
    ControlPersist 1h
EOF
```

The next git operation opens the connection, with the usual touch, and those of the following hour reuse it. While it is open, any program running as you can push through it, so close it when you step away:

```bash
ssh -O check git@github.com   # is a connection open?
ssh -O exit git@github.com    # close it now
```

The OpenSSH that ships with Windows cannot share connections; inside WSL2, this setup works as on Linux.

## Request access

Pull requests are open to organization members, so the team adds you. Ask a maintainer on [Discord](./discord) for:

- membership in the [`a-novel`](https://github.com/a-novel) and [`a-novel-kit`](https://github.com/a-novel-kit) organizations;
- the developer teams of the areas you work in, named `developers-<area>`.

The area is the kind of repository:

| Area        | Repositories                         |
| ----------- | ------------------------------------ |
| `service`   | The backend services, `service-*`    |
| `platform`  | The frontend platforms, `platform-*` |
| `library`   | The shared libraries and the stack   |
| `infra`     | The infrastructure code              |
| `workflows` | The shared CI actions                |
| `meta`      | Each organization's `.github`        |

Send your GitHub username with the request:

```bash
gh api user --jq .login
```

GitHub then emails an invitation per organization. Accept both, from the email or the invitation pages ([`a-novel`](https://github.com/orgs/a-novel/invitation), [`a-novel-kit`](https://github.com/orgs/a-novel-kit/invitation)).
