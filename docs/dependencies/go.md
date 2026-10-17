# Go

[Go](https://go.dev) builds the services, libraries and CLI. Any Go from 1.21 on works: it downloads the newer toolchain a repository asks for.

## Install

Skip to [Configure](#configure) if this prints `go1.21` or newer:

```bash
go version
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

## Configure

### Allow toolchain downloads

Skip if this prints `auto`:

```bash
go env GOTOOLCHAIN
```

Otherwise, your distribution turned downloads off. Turn them back on:

```bash
go env -w GOTOOLCHAIN=auto
```

### Put Go programs on your PATH

The CLI installs into Go's program directory, `~/go/bin` unless you moved it. Skip if `echo $PATH` lists it already.

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
