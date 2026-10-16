# Podman

[Podman](https://podman.io) runs the local infrastructure, such as Postgres, and the services' containers, rootless and without a daemon. `podman compose` hands compose files to [podman-compose](https://github.com/containers/podman-compose), which must be 1.6.0 or newer: older releases mishandle health-gated start-up and can hang.

## Install

Skip to [Configure](#configure) if both commands answer and podman-compose reports 1.6.0 or newer:

```bash
podman --version
podman compose version
```

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu
The distribution's podman-compose is older than 1.6.0; [pipx](https://pipx.pypa.io) installs the current release:

```bash
sudo apt install -y podman pipx
pipx install podman-compose
pipx ensurepath
exec "$SHELL"
```

== Fedora

```bash
sudo dnf install -y podman podman-compose
```

== openSUSE

```bash
sudo zypper install -y podman podman-compose
```

== Arch
Pick `crun` when pacman asks for an OCI runtime.

```bash
sudo pacman -S --needed podman podman-compose
```

:::
=== macOS

```bash
brew install podman podman-compose
```

=== Windows
Podman runs its containers in a WSL2 virtual machine. podman-compose is a Python program, installed through pipx:

```powershell
winget install --id RedHat.Podman -e
winget install --id Python.Python.3.14 -e
py -m pip install --user pipx
py -m pipx ensurepath
```

Then, in a new terminal:

```powershell
pipx install podman-compose
```

::::

## Configure

:::tabs key:os
== Linux
Podman runs natively, so there is nothing to start. It runs container health checks with systemd timers, which WSL2 leaves off on older installs. Check that systemd runs:

```bash
ps -p 1 -o comm=
```

Anything but `systemd` means it is off. Turn it on, then restart WSL with `wsl --shutdown` from Windows:

```bash
printf '[boot]\nsystemd=true\n' | sudo tee -a /etc/wsl.conf
```

== macOS
Containers run in a Podman machine, a Linux virtual machine. Create it once, then start it after each reboot:

```bash
podman machine init
podman machine start
```

== Windows
Containers run in a Podman machine, a Linux virtual machine. Create it once, then start it after each reboot:

```powershell
podman machine init
podman machine start
```

:::

Check that Podman runs:

```bash
podman info --format '{{.Version.Version}}'
```

It prints the Podman version; an error means Podman, or its machine, is not running. `podman compose version` must name `podman-compose` as the external compose provider: another provider installed first, such as docker-compose, takes precedence.
