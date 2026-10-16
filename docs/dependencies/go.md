# Go

[Go](https://go.dev) builds the services, libraries and CLI. Use at least the version in [`cli/go.mod`](https://github.com/a-novel-kit/stack/blob/master/cli/go.mod), which tracks the latest release.

## Install

Skip to [Configure](#configure) if your Go meets the version the second command prints:

```bash
go version
curl -fsSL https://raw.githubusercontent.com/a-novel-kit/stack/master/cli/go.mod | grep '^go '
```

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu
The distribution's Go lags behind; install the official build:

```bash
GO_VERSION=$(curl -fsSL "https://go.dev/VERSION?m=text" | head -n 1)
GO_ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -fsSLo /tmp/go.tar.gz "https://go.dev/dl/$GO_VERSION.linux-$GO_ARCH.tar.gz"
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH="$PATH:/usr/local/go/bin"' >> "$HOME/.$(basename "$SHELL")rc"
exec "$SHELL"
```

To upgrade, run the first four lines again.
== Fedora
The distribution's Go lags behind; install the official build:

```bash
GO_VERSION=$(curl -fsSL "https://go.dev/VERSION?m=text" | head -n 1)
GO_ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -fsSLo /tmp/go.tar.gz "https://go.dev/dl/$GO_VERSION.linux-$GO_ARCH.tar.gz"
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH="$PATH:/usr/local/go/bin"' >> "$HOME/.$(basename "$SHELL")rc"
exec "$SHELL"
```

To upgrade, run the first four lines again.
== openSUSE

```bash
sudo zypper install -y go
```

== Arch

```bash
sudo pacman -S --needed go
```

:::
=== macOS

```bash
brew install go
```

=== Windows

```powershell
winget install --id GoLang.Go -e
```

::::

::: tip An older Go still works
It fetches the toolchain the CLI needs, unless your distribution disabled that: `go env -w GOTOOLCHAIN=auto` turns it back on.
:::

## Configure

Put Go's program directory, where the CLI installs, on your `PATH`:

:::tabs key:shell
== Linux / macOS

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> "$HOME/.$(basename "$SHELL")rc"
exec "$SHELL"
```

== Windows

```powershell
$GoBin = Join-Path (go env GOPATH) "bin"
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$GoBin*") { [Environment]::SetEnvironmentVariable("Path", "$UserPath;$GoBin", "User") }
```

Open a new terminal to pick up the change.
:::
