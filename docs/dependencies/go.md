# Go

[Go](https://go.dev) builds the services, the shared libraries and the `a-novel` CLI. The CLI needs at least the Go version its [`cli/go.mod`](https://github.com/a-novel-kit/stack/blob/master/cli/go.mod) names, and it tracks the latest release.

## Install

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu
The distribution's Go trails the release the CLI needs; install the official build:

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
The distribution's Go trails the release the CLI needs; install the official build:

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

```bash
go version
```

::: tip An older Go still works
A Go older than `cli/go.mod` downloads the toolchain the CLI asks for, unless your distribution switched that off. Switch it back on with `go env -w GOTOOLCHAIN=auto`.
:::

## Configure

`go install` writes programs to `$(go env GOPATH)/bin`, `~/go/bin` by default, which is where the `a-novel` CLI lands. Put that directory on your `PATH`:

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
