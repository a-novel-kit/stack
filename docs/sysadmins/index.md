# Sysadmins

Sysadmins hold admin rights over the repositories and the cloud, and keep the organizations' credentials. On top of the contributor setup, they meet stricter security requirements and add two tools: the [Google Cloud CLI](./gcloud) and [Bitwarden](./bitwarden).

## Security requirements

- **A FIDO2 security key, with a backup.** Your SSH key, for GitHub and commit signing, lives on a hardware key, as [set up below](#github-with-a-security-key). Key files are not allowed.
- **One shared connection.** Set up [connection sharing](/dependencies/github#share-one-connection), and close it when you leave your machine.
- **Elevated scopes for one task only.** Keep your `gh` session on its default scopes. Add `admin:org` or another for a single run, then remove it.
- **No credential in clear.** Keep tokens in the CLI's encrypted store, never in a file or your shell history.

## GitHub with a security key

This replaces the key file of the contributor setup. Plug the key in; on macOS, run `brew install openssh` first, as the system OpenSSH lacks security key support.

:::tabs key:shell
== Linux / macOS

```bash
ssh-keygen -t ed25519-sk -C "$(git config user.email)" -f ~/.ssh/id_ed25519_sk
gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key
gh ssh-key add ~/.ssh/id_ed25519_sk.pub --type authentication --title "$(hostname)"
gh ssh-key add ~/.ssh/id_ed25519_sk.pub --type signing --title "$(hostname)"
gh auth refresh -h github.com --remove-scopes admin:public_key,admin:ssh_signing_key
git config --global user.signingkey ~/.ssh/id_ed25519_sk.pub
```

== Windows
Windows OpenSSH, which Git already uses, reaches the key through Windows Hello.

```powershell
ssh-keygen -t ed25519-sk -C (git config user.email) -f "$HOME\.ssh\id_ed25519_sk"
gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key
gh ssh-key add "$HOME\.ssh\id_ed25519_sk.pub" --type authentication --title $env:COMPUTERNAME
gh ssh-key add "$HOME\.ssh\id_ed25519_sk.pub" --type signing --title $env:COMPUTERNAME
gh auth refresh -h github.com --remove-scopes admin:public_key,admin:ssh_signing_key
git config --global user.signingkey "$HOME\.ssh\id_ed25519_sk.pub"
```

:::

Set up the backup key the same way, with `-f ~/.ssh/id_ed25519_sk_backup`, list both keys as `IdentityFile` under `Host github.com` in `~/.ssh/config`, and add both to your [allowed signers](/dependencies/signing#check-a-signature). Then remove the key file from your GitHub account. Each signature now asks for a touch, as does each new connection; the [shared connection](/dependencies/github#share-one-connection) makes those rare.

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
