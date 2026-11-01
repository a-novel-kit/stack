# Node.js and pnpm

[Node.js](https://nodejs.org) runs the JavaScript tooling and frontends, and [pnpm](https://pnpm.io) manages their packages; the repositories refuse npm and yarn.

## Install

The third command prints the minimum Node.js version. Skip to [Configure](#configure) if pnpm prints 11.20 or newer and Node.js meets that minimum:

```bash
node --version
pnpm --version
curl -fsSL https://raw.githubusercontent.com/a-novel-kit/stack/master/package.json | grep '"node"'
```

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu
The distribution's Node.js is too old; use [nvm](https://github.com/nvm-sh/nvm):

```bash
curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.8/install.sh | bash
. "$HOME/.nvm/nvm.sh"
nvm install node
npm install --global pnpm
```

== Fedora
Use [nvm](https://github.com/nvm-sh/nvm):

```bash
curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.8/install.sh | bash
. "$HOME/.nvm/nvm.sh"
nvm install node
npm install --global pnpm
```

== openSUSE
The distribution's pnpm is too old. Install it with npm into `~/.local/bin`, which openSUSE adds to the `PATH` when you log in:

```bash
sudo zypper install -y nodejs-default npm-default
npm install --global --prefix ~/.local pnpm
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

Each repository pins its pnpm version, and pnpm switches to it. pnpm 11 releases before 11.20 rewrite `pnpm-lock.yaml` as they switch.

## Configure

The team's packages live on [GitHub Packages](https://docs.github.com/packages), which needs a token even to read.

Skip this section if this prints a version:

```bash
npm view @a-novel-kit/nodelib-config version --registry=https://npm.pkg.github.com
```

1. [Create a classic token](https://github.com/settings/tokens/new?scopes=read:packages&description=a-novel%20packages) with only the `read:packages` scope, and copy it.
2. Save it in your home `.npmrc`, never a repository's:

:::tabs key:shell
== Linux / macOS

```bash
read -rs GITHUB_PACKAGES_TOKEN   # paste the token, then press Enter
echo "//npm.pkg.github.com/:_authToken=$GITHUB_PACKAGES_TOKEN" >> ~/.npmrc
chmod 600 ~/.npmrc
unset GITHUB_PACKAGES_TOKEN
```

== Windows

```powershell
$Token = Read-Host -MaskInput "GitHub Packages token"
Add-Content "$HOME\.npmrc" "//npm.pkg.github.com/:_authToken=$Token"
Remove-Variable Token
```

:::

Run the check again to confirm. Platform image builds also need the token in the [CLI's secrets](/cli/secrets#the-github-packages-token).
