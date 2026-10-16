# Sysadmins

Sysadmins hold admin rights over the repositories and the cloud, and keep the organizations' credentials. On top of the contributor setup, they meet stricter security requirements and add two tools: the [Google Cloud CLI](./gcloud) and [Bitwarden](./bitwarden).

## Security requirements

- **A FIDO2 security key, with a backup.** Your SSH key, for GitHub and commit signing, lives on a hardware key: follow the Security key tab of the [GitHub](/dependencies/github#add-an-ssh-key) page, then register a second key. Key files are not allowed.
- **One shared connection.** Set up [connection sharing](/dependencies/github#share-one-connection), and close it when you leave your machine.
- **Elevated scopes for one task only.** Keep your `gh` session on its default scopes. Add `admin:org` or another for a single run, then remove it.
- **No credential in clear.** Keep tokens in the CLI's encrypted store, never in a file or your shell history.

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
