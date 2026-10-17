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

Set the name and email your commits carry. Use your GitHub username as the name. GitHub links a commit to your account by its email alone. Use an address added and verified in your [email settings](https://github.com/settings/emails), or the private `…@users.noreply.github.com` address shown there under **Keep my email addresses private**.

Skip if `git config --global user.email` already prints that address.

:::tabs key:shell
== Linux / macOS

```bash
GIT_NAME="your-github-username"
GIT_EMAIL="you@example.com"
git config --global user.name "$GIT_NAME"
git config --global user.email "$GIT_EMAIL"
```

== Windows

```powershell
$GitName = "your-github-username"
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
