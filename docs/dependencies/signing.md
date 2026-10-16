# Commit signing

A signed commit proves it was made with your key, and GitHub marks it **Verified**. The protected branches of every A-Novel repository accept verified commits only, so sign every commit you make. Git signs with the SSH key from the [GitHub](./github#add-an-ssh-key) page; no GPG is involved.

Skip this page if your commits already show **Verified** on GitHub. Otherwise pick the tab of the key you created there; a key at another path works the same, with its path in place of the one shown. With a security key, each signature asks for a touch.

## Configure Git

::::tabs key:sshkey
=== Security key
:::tabs key:shell
== Linux / macOS

```bash
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519_sk.pub
git config --global commit.gpgsign true
git config --global tag.gpgsign true
```

== Windows
The first command makes Git sign with Windows OpenSSH, the client that reaches security keys.

```powershell
git config --global gpg.ssh.program "C:/Windows/System32/OpenSSH/ssh-keygen.exe"
git config --global gpg.format ssh
git config --global user.signingkey "$HOME\.ssh\id_ed25519_sk.pub"
git config --global commit.gpgsign true
git config --global tag.gpgsign true
```

:::
=== Key file
:::tabs key:shell
== Linux / macOS

```bash
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519.pub
git config --global commit.gpgsign true
git config --global tag.gpgsign true
```

== Windows

```powershell
git config --global gpg.format ssh
git config --global user.signingkey "$HOME\.ssh\id_ed25519.pub"
git config --global commit.gpgsign true
git config --global tag.gpgsign true
```

:::
::::

## Register the key for signing

GitHub verifies a signature only against a key registered as a signing key, even when the same key already authenticates you. Register it a second time, with the signing type:

::::tabs key:sshkey
=== Security key
:::tabs key:shell
== Linux / macOS

```bash
gh auth refresh -h github.com -s admin:ssh_signing_key
gh ssh-key add ~/.ssh/id_ed25519_sk.pub --type signing --title "$(hostname)"
gh auth refresh -h github.com --remove-scopes admin:ssh_signing_key
```

== Windows

```powershell
gh auth refresh -h github.com -s admin:ssh_signing_key
gh ssh-key add "$HOME\.ssh\id_ed25519_sk.pub" --type signing --title $env:COMPUTERNAME
gh auth refresh -h github.com --remove-scopes admin:ssh_signing_key
```

:::
=== Key file
:::tabs key:shell
== Linux / macOS

```bash
gh auth refresh -h github.com -s admin:ssh_signing_key
gh ssh-key add ~/.ssh/id_ed25519.pub --type signing --title "$(hostname)"
gh auth refresh -h github.com --remove-scopes admin:ssh_signing_key
```

== Windows

```powershell
gh auth refresh -h github.com -s admin:ssh_signing_key
gh ssh-key add "$HOME\.ssh\id_ed25519.pub" --type signing --title $env:COMPUTERNAME
gh auth refresh -h github.com --remove-scopes admin:ssh_signing_key
```

:::
::::

A backup security key signs too once you register it the same way.

## Check a signature

Git checks SSH signatures against a list of trusted keys, the allowed signers file. Add your key to it, then sign a throwaway commit:

::::tabs key:sshkey
=== Security key
:::tabs key:shell
== Linux / macOS

```bash
mkdir -p ~/.config/git
echo "$(git config user.email) namespaces=\"git\" $(cat ~/.ssh/id_ed25519_sk.pub)" >> ~/.config/git/allowed_signers
git config --global gpg.ssh.allowedSignersFile ~/.config/git/allowed_signers
cd "$(mktemp -d)" && git init -q && git commit -q --allow-empty -m "test: signing" && git log --show-signature -1
```

== Windows

```powershell
New-Item -ItemType Directory -Force "$HOME\.config\git" | Out-Null
Add-Content "$HOME\.config\git\allowed_signers" "$(git config user.email) namespaces=`"git`" $(Get-Content "$HOME\.ssh\id_ed25519_sk.pub")"
git config --global gpg.ssh.allowedSignersFile "$HOME\.config\git\allowed_signers"
Set-Location (New-Item -ItemType Directory (Join-Path ([IO.Path]::GetTempPath()) (New-Guid)))
git init -q; git commit -q --allow-empty -m "test: signing"; git log --show-signature -1
```

:::
=== Key file
:::tabs key:shell
== Linux / macOS

```bash
mkdir -p ~/.config/git
echo "$(git config user.email) namespaces=\"git\" $(cat ~/.ssh/id_ed25519.pub)" >> ~/.config/git/allowed_signers
git config --global gpg.ssh.allowedSignersFile ~/.config/git/allowed_signers
cd "$(mktemp -d)" && git init -q && git commit -q --allow-empty -m "test: signing" && git log --show-signature -1
```

== Windows

```powershell
New-Item -ItemType Directory -Force "$HOME\.config\git" | Out-Null
Add-Content "$HOME\.config\git\allowed_signers" "$(git config user.email) namespaces=`"git`" $(Get-Content "$HOME\.ssh\id_ed25519.pub")"
git config --global gpg.ssh.allowedSignersFile "$HOME\.config\git\allowed_signers"
Set-Location (New-Item -ItemType Directory (Join-Path ([IO.Path]::GetTempPath()) (New-Guid)))
git init -q; git commit -q --allow-empty -m "test: signing"; git log --show-signature -1
```

:::
::::

The log opens with `Good "git" signature for you@example.com`. On GitHub, your next pushed commit shows **Verified**.

## Keep commits signed

A commit stays signed until something rewrites it without your key. GitHub's **Update branch** button is the usual culprit: its merge variant adds a merge commit the repositories refuse, and its rebase variant rewrites your commits unsigned, so the merge queue rejects the pull request. Bring a branch up to date locally instead; the rebase signs every commit it replays:

```bash
git fetch origin
git rebase origin/master
git push --force-with-lease
```
