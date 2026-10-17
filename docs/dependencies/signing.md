# Commit signing

Every repository requires signed commits, and protected branches refuse the rest. Git signs them with the SSH key from the [GitHub](./github#add-an-ssh-key) page.

Skip this page if your commits already show **Verified** on GitHub. Using another key? Put its path in place of `~/.ssh/id_ed25519.pub`.

## Configure Git

:::tabs key:shell
== Linux / macOS

```bash
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519.pub
git config --global commit.gpgsign true
git config --global tag.gpgsign true
```

== Windows
The last line makes Git sign with Windows OpenSSH, which reaches your SSH agent.

```powershell
git config --global gpg.format ssh
git config --global user.signingkey "$HOME\.ssh\id_ed25519.pub"
git config --global commit.gpgsign true
git config --global tag.gpgsign true
git config --global gpg.ssh.program "C:/Windows/System32/OpenSSH/ssh-keygen.exe"
```

:::

## Register the key for signing

GitHub checks signatures against signing keys only. Register your key again, as a signing key:

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

## Check a signature

Trust your key locally, then sign a test commit:

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

The log shows `Good "git" signature`, and GitHub marks your next push **Verified**.

## Keep commits signed

GitHub's **Update branch** button rewrites commits without your key, and the repositories refuse the result. Rebase locally instead:

```bash
git fetch origin
git rebase origin/master
git push --force-with-lease
```
