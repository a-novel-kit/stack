# Git

[Git](https://git-scm.com) versions every repository.

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

Set the name and email your commits carry. Use an email verified on GitHub, or your private [noreply address](https://github.com/settings/emails).

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

The stack repository contains a symbolic link. On Windows, turn on Developer Mode from an administrator PowerShell, then let Git create links:

```powershell
reg add "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock" /t REG_DWORD /f /v AllowDevelopmentWithoutDevLicense /d 1
git config --global core.symlinks true
```
