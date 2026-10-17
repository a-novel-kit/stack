# Sysadmins

Sysadmins hold admin rights over the repositories and the cloud, and keep the organizations' credentials. On top of the contributor setup, they meet stricter security requirements and add two tools: the [Google Cloud CLI](./gcloud) and [Bitwarden](./bitwarden).

## Security requirements

- **A FIDO2 security key, with a backup.** Your SSH keys, for GitHub and commit signing, live on a hardware key, as [set up below](#github-with-a-security-key). Key files are not allowed.
- **Nothing open when you leave.** Set up [connection sharing](/dependencies/github#share-one-connection). Close the connection and unplug your security key whenever you leave your machine, since its signing key needs no touch.
- **Elevated scopes for one task only.** Keep your `gh` session on its default scopes. Add `admin:org` or another for a single run, then remove it.
- **No credential in clear.** Keep tokens in the CLI's encrypted store, never in a file or your shell history.

## GitHub with a security key

This replaces the key file of the contributor setup. Each security key holds two SSH keys. The authentication key asks for a touch on every new connection to GitHub. The signing key signs without one, so commits, rebases and agents never wait on you.

Plug the key in. On macOS, run `brew install openssh` first, as the system OpenSSH lacks security key support. Leave the signing key's passphrase empty, or every commit asks for it.

:::tabs key:shell
== Linux / macOS

```bash
EMAIL=$(git config user.email)
ssh-keygen -t ed25519-sk -C "$EMAIL" -f ~/.ssh/id_ed25519_sk
ssh-keygen -t ed25519-sk -O no-touch-required -C "$EMAIL" -f ~/.ssh/id_ed25519_sk_signing
gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key
gh ssh-key add ~/.ssh/id_ed25519_sk.pub --type authentication --title "$(hostname)"
gh ssh-key add ~/.ssh/id_ed25519_sk_signing.pub --type signing --title "$(hostname)"
gh auth refresh -h github.com --remove-scopes admin:public_key,admin:ssh_signing_key
git config --global user.signingkey ~/.ssh/id_ed25519_sk_signing.pub
```

== Windows
Windows OpenSSH reaches the key through Windows Hello, which may still ask for a touch on each signature. The two `git config` lines point Git at it.

```powershell
$Email = git config user.email
git config --global core.sshCommand "C:/Windows/System32/OpenSSH/ssh.exe"
git config --global gpg.ssh.program "C:/Windows/System32/OpenSSH/ssh-keygen.exe"
ssh-keygen -t ed25519-sk -C $Email -f "$HOME\.ssh\id_ed25519_sk"
ssh-keygen -t ed25519-sk -O no-touch-required -C $Email -f "$HOME\.ssh\id_ed25519_sk_signing"
gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key
gh ssh-key add "$HOME\.ssh\id_ed25519_sk.pub" --type authentication --title $env:COMPUTERNAME
gh ssh-key add "$HOME\.ssh\id_ed25519_sk_signing.pub" --type signing --title $env:COMPUTERNAME
gh auth refresh -h github.com --remove-scopes admin:public_key,admin:ssh_signing_key
git config --global user.signingkey "$HOME\.ssh\id_ed25519_sk_signing.pub"
```

:::

Set up the backup key the same way, with `_backup` at the end of both file names. List both authentication keys as `IdentityFile` under `Host github.com` in `~/.ssh/config`, and add both signing keys to your [allowed signers](/dependencies/signing#check-a-signature). When you work with the backup, point `user.signingkey` at its signing key. Then remove the key file from your GitHub account.

## Duties

- **Enroll contributors:** give them the member role on Discord, invite them to both GitHub organizations and to the teams of their repositories, and add them to the CI mention map in Bitwarden.
- **Apply repository settings** with [`repo update`](./repositories).
- **Operate production:** follow the [infra runbooks](https://github.com/a-novel/infra/tree/master/docs/runbooks).

### Set up the bot for an organization

Each organization has its own GitHub App, which posts the [bot comments](/cli/bot-comments). These steps serve a new organization or a re-created App.

1. [Register a GitHub App](https://docs.github.com/apps/creating-github-apps/registering-a-github-app/registering-a-github-app) on the organization, with only the repository permissions **Issues: read and write**, **Pull requests: read and write** and **Metadata: read-only**. Disable the webhook, then install the App on every repository.
2. Store the App's client ID as the organization Actions variable `AGENT_BOT_CLIENT_ID`.
3. Generate a private key and store it as the organization Actions secret `AGENT_BOT_PRIVATE_KEY`, visible to the dispatcher repository. The key never goes to a developer machine.
4. Comment on a scratch pull request to check.
