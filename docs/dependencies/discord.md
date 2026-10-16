# Discord

The team talks on Discord, and CI posts there: your pull requests' progress and reviews, failed builds on `master`, and releases. The browser app works as well as the [desktop app](https://discord.com/download).

## Join

Joining needs the browser or the app: open the [A-Novel server invite](https://discord.gg/rp4Qr8cA) and accept it.

## Get mentioned

CI mentions you when your pull requests move and when someone requests your review, and it mentions a repository's role when that repository's `master` build fails. Both need a maintainer to link your accounts, from IDs you send.

1. Turn on Developer Mode in Discord: **User Settings**, then **Advanced**, then **Developer Mode**. Discord only shows IDs in this mode.
2. Click your avatar, then **Copy User ID**.
3. Print your GitHub username:

   ```bash
   gh api user --jq .login
   ```

4. Send a maintainer, in a direct message:
   - your GitHub username;
   - your Discord user ID;
   - the repositories you work on, for their roles.

Until the link exists, CI posts your updates without the mention.
