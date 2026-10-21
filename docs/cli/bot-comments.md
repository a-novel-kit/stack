# Comment as the bot

Automated notes, such as an agent's review replies, post as the organization's bot rather than as you. The bot can only comment. Its key stays in CI, behind a workflow in each organization's dispatcher repository:

| Organization  | Dispatcher repository |
| ------------- | --------------------- |
| `a-novel-kit` | `a-novel-kit/stack`   |
| `a-novel`     | `a-novel/.github`     |

Posting needs write access to the dispatcher repository; ask the team if you lack it.

## Post a comment

`NUMBER` is a pull request or an issue:

```bash
ORG=a-novel
REPO=service-authentication
NUMBER=123
a-novel core bot-comment "$ORG" "$REPO" "$NUMBER" --body "Automated review note."
```

To reply in a review thread, find the comment's ID first:

```bash
gh api "repos/$ORG/$REPO/pulls/$NUMBER/comments" --jq '.[] | {id, path, body}'
```

```bash
REPLY_TO=2103847559
a-novel core bot-comment "$ORG" "$REPO" "$NUMBER" --reply-to "$REPLY_TO" --body "Fixed in abc1234."
```

## Post many at once

```bash
echo '[{"number": 123, "body": "First note."}, {"number": 123, "reply_to": 2103847559, "body": "Fixed."}]' \
  | a-novel core bot-comment "$ORG" "$REPO" --batch -
```
