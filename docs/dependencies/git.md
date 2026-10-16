# Git

[Git](https://git-scm.com) versions every A-Novel repository. The CLI clones them over SSH, which the [GitHub](./github) page sets up.

## Install

Skip to [Configure](#configure) if this already prints a version:

```bash
git --version
```

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu

```bash
sudo apt install -y git
```

== Fedora

```bash
sudo dnf install -y git
```

== openSUSE

```bash
sudo zypper install -y git
```

== Arch

```bash
sudo pacman -S --needed git
```

:::
=== macOS

```bash
brew install git
```

=== Windows

```powershell
winget install --id Git.Git -e
```

::::

## Configure

Set the name and email your commits carry. Use an email verified on your GitHub account, so GitHub links the commits to you; the private `…@users.noreply.github.com` address from your [email settings](https://github.com/settings/emails) works too. Edit the two variables, then run the block as is.

:::tabs key:shell
== Linux / macOS

```bash
GIT_NAME="Your Name"
GIT_EMAIL="you@example.com"
git config --global user.name "$GIT_NAME"
git config --global user.email "$GIT_EMAIL"
```

== Windows

```powershell
$GitName = "Your Name"
$GitEmail = "you@example.com"
git config --global user.name $GitName
git config --global user.email $GitEmail
```

:::

### Symbolic links on Windows

The stack repository stores a symbolic link, which Git on Windows checks out as a plain file unless Developer Mode is on. Turn it on from a PowerShell run as administrator, then let Git create links:

```powershell
reg add "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock" /t REG_DWORD /f /v AllowDevelopmentWithoutDevLicense /d 1
git config --global core.symlinks true
```

Linux and macOS need nothing here.
