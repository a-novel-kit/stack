# Podman

[Podman](https://podman.io) runs the local databases and services in containers. It needs [podman-compose](https://github.com/containers/podman-compose) 1.6.0 or newer; older releases can hang on start-up.

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
The distribution's podman-compose is too old; use [pipx](https://pipx.pypa.io):

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
Podman runs containers in a WSL2 machine, and podman-compose comes from pipx:

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
Nothing to start on Linux. Under WSL2, health checks need systemd, and this prints `systemd` when it runs:

```bash
ps -p 1 -o comm=
```

Otherwise, turn it on, then restart WSL from Windows with `wsl --shutdown`:

```bash
printf '[boot]\nsystemd=true\n' | sudo tee -a /etc/wsl.conf
```

== macOS
Create the Podman machine once, and start it after each reboot:

```bash
podman machine init
podman machine start
```

== Windows
Create the Podman machine once, and start it after each reboot:

```powershell
podman machine init
podman machine start
```

:::

Check that Podman runs:

```bash
podman info --format '{{.Version.Version}}'
```

An error means Podman or its machine is down. `podman compose version` must name podman-compose, since an installed docker-compose takes precedence.
