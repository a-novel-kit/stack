# Bitwarden

[Bitwarden Secrets Manager](https://bitwarden.com/products/secrets-manager/) holds the organizations' shared credentials, in the `a-novel` and `a-novel-kit` projects, and `github-discord-map`, which CI reads to mention members on Discord. The governance keys every repository holds come from Google Cloud's Secret Manager instead, as infra's [Governance secrets](https://github.com/a-novel/infra/blob/master/docs/runbooks/secrets.md#governance-secrets) runbook describes.

Sysadmins work in the [Bitwarden web app](https://vault.bitwarden.eu). Ask another sysadmin to [open it to you](./promote-member#open-bitwarden-to-them).
