# Comment as the bot

Automated notes, such as an AI agent's review replies, post as each organization's bot rather than as you, so readers can tell them from a person's words. The bot can only comment: opening, editing and merging pull requests always happen under your own `gh` account.

The bot's key never reaches your machine: `a-novel core bot-comment` asks a workflow in the organization's dispatcher repository to post for you, and only that workflow holds the key.

| Organization  | Dispatcher repository |
| ------------- | --------------------- |
| `a-novel-kit` | `a-novel-kit/stack`   |
| `a-novel`     | `a-novel/.github`     |

## Request access

Dispatching that workflow takes the `actions: write` permission on the dispatcher repository. Ask a maintainer for it on both dispatcher repositories, with your GitHub username (`gh api user --jq .login`).

## Post a comment

Set the target, then post. `NUMBER` is a pull request or an issue:

```bash
ORG=a-novel
REPO=service-authentication
NUMBER=123
a-novel core bot-comment "$ORG" "$REPO" "$NUMBER" --body "Automated review note."
```

The command waits for the workflow and fails, with the failed step's log, if the comment did not post.

To reply inside a pull request's review thread, name the review comment you answer. Its ID comes from the pull request's review comments:

```bash
gh api "repos/$ORG/$REPO/pulls/$NUMBER/comments" --jq '.[] | {id, path, body}'
```

```bash
REPLY_TO=2103847559
a-novel core bot-comment "$ORG" "$REPO" "$NUMBER" --reply-to "$REPLY_TO" --body "Fixed in abc1234."
```

An issue has no review threads: answer an issue comment with a new comment.

## Post many comments at once

`--batch` posts a JSON array of comments on one repository in a single run, read from a file or from standard input:

```bash
echo '[{"number": 123, "body": "First note."}, {"number": 123, "reply_to": 2103847559, "body": "Fixed."}]' \
  | a-novel core bot-comment "$ORG" "$REPO" --batch -
```

::: details Owners: set up the bot for an organization
Each organization has its own GitHub App, because an App's key is an organization secret, and organization secrets stay inside their organization. Both organizations are set up; these steps serve a new organization or a re-created App.

1. [Register a GitHub App](https://docs.github.com/apps/creating-github-apps/registering-a-github-app/registering-a-github-app) on the organization. Grant only the repository permissions **Issues: read and write**, **Pull requests: read and write** and **Metadata: read-only**, so the bot cannot push, merge or release. Disable the webhook, then install the App on all the organization's repositories.
2. Store the App's client ID, which is public, as the organization Actions variable `AGENT_BOT_CLIENT_ID`.
3. Generate a private key from the App's settings page and store it as the organization Actions secret `AGENT_BOT_PRIVATE_KEY`, visible to the dispatcher repository. The key never goes to a developer machine.
4. Check end to end by commenting on a scratch pull request or issue.

:::
