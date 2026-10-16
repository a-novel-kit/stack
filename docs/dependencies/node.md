# Node.js and pnpm

[Node.js](https://nodejs.org) runs the JavaScript tooling (linters, formatters, client packages) and the frontend platforms. [pnpm](https://pnpm.io) is the package manager of every JavaScript workspace; the repositories refuse npm and yarn. Node.js must meet `engines.node` in the stack's [`package.json`](https://github.com/a-novel-kit/stack/blob/master/package.json).

## Install

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu
The distribution's Node.js is too old. [nvm](https://github.com/nvm-sh/nvm) installs the current release, then npm installs pnpm:

```bash
curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.8/install.sh | bash
. "$HOME/.nvm/nvm.sh"
nvm install node
npm install --global pnpm
```

== Fedora
[nvm](https://github.com/nvm-sh/nvm) installs the current Node.js release, then npm installs pnpm:

```bash
curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.8/install.sh | bash
. "$HOME/.nvm/nvm.sh"
nvm install node
npm install --global pnpm
```

== openSUSE

```bash
sudo zypper install -y nodejs-default npm-default pnpm
```

== Arch

```bash
sudo pacman -S --needed nodejs npm pnpm
```

:::
=== macOS

```bash
brew install node pnpm
```

=== Windows

```powershell
winget install --id OpenJS.NodeJS -e
winget install --id pnpm.pnpm -e
```

::::

```bash
node --version
pnpm --version
```

Each repository pins its pnpm version, and pnpm switches to it on its own: inside a repository, `pnpm --version` prints the pinned version once pnpm has fetched it.

## Configure

The `@a-novel` and `@a-novel-kit` packages live on [GitHub Packages](https://docs.github.com/packages), which asks for a token even to read. Give pnpm one with the `read:packages` scope only.

1. Create a classic token on GitHub; this step needs the browser. The link preselects the scope: [new token with `read:packages`](https://github.com/settings/tokens/new?scopes=read:packages&description=a-novel%20packages). Pick an expiry, generate it, and copy it.
2. Store it in the `.npmrc` of your home directory. A repository's `.npmrc` is committed, so the token never goes there.

:::tabs key:shell
== Linux / macOS
The token is read with no echo, so it lands in neither the terminal nor your shell history:

```bash
read -rs GITHUB_PACKAGES_TOKEN   # paste the token, then press Enter
echo "//npm.pkg.github.com/:_authToken=$GITHUB_PACKAGES_TOKEN" >> ~/.npmrc
chmod 600 ~/.npmrc
unset GITHUB_PACKAGES_TOKEN
```

== Windows
The token is read masked, so it lands in neither the terminal nor your shell history:

```powershell
$Token = Read-Host -MaskInput "GitHub Packages token"
Add-Content "$HOME\.npmrc" "//npm.pkg.github.com/:_authToken=$Token"
Remove-Variable Token
```

:::

Check that the registry accepts it:

```bash
npm view @a-novel-kit/nodelib-config version --registry=https://npm.pkg.github.com
```

It prints the latest version of a team package. Platform repositories also read this token when they build images: the [secrets runbook](/cli/secrets#the-github-packages-token) stores a copy for the CLI.
