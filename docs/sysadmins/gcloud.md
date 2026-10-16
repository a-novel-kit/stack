# Google Cloud CLI

Production runs on Google Cloud, which sysadmins operate with the [Google Cloud CLI](https://cloud.google.com/sdk/docs/install), `gcloud`, as the [infra runbooks](https://github.com/a-novel/infra/tree/master/docs/runbooks) describe.

## Install

Skip to [Configure](#configure) if this prints a version:

```bash
gcloud --version
```

::::tabs key:os
=== Linux
:::tabs key:distro
== Debian / Ubuntu
Use Google's repository:

```bash
curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg
echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | sudo tee /etc/apt/sources.list.d/google-cloud-sdk.list > /dev/null
sudo apt update && sudo apt install -y google-cloud-cli
```

== Fedora
Use Google's repository:

```bash
sudo tee /etc/yum.repos.d/google-cloud-sdk.repo > /dev/null <<EOF
[google-cloud-cli]
name=Google Cloud CLI
baseurl=https://packages.cloud.google.com/yum/repos/cloud-sdk-el10-$(uname -m)
enabled=1
gpgcheck=1
gpgkey=https://packages.cloud.google.com/yum/doc/rpm-package-key-v10.gpg
EOF
sudo dnf install -y google-cloud-cli
```

== openSUSE
The distribution has no package; install Google's archive:

```bash
GCLOUD_ARCH=$(uname -m | sed 's/aarch64/arm/')
curl -fsSLo /tmp/gcloud.tar.gz "https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-linux-$GCLOUD_ARCH.tar.gz"
tar -xf /tmp/gcloud.tar.gz -C "$HOME"
"$HOME/google-cloud-sdk/install.sh" --quiet --path-update true --rc-path "$HOME/.$(basename "$SHELL")rc"
exec "$SHELL"
```

== Arch
Only the AUR packages it; install Google's archive:

```bash
GCLOUD_ARCH=$(uname -m | sed 's/aarch64/arm/')
curl -fsSLo /tmp/gcloud.tar.gz "https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-linux-$GCLOUD_ARCH.tar.gz"
tar -xf /tmp/gcloud.tar.gz -C "$HOME"
"$HOME/google-cloud-sdk/install.sh" --quiet --path-update true --rc-path "$HOME/.$(basename "$SHELL")rc"
exec "$SHELL"
```

:::
=== macOS

```bash
brew install --cask gcloud-cli
```

=== Windows

```powershell
winget install --id Google.CloudSDK -e
```

::::

## Configure

Ask an owner for your roles on the management and production projects, then sign in with that Google account:

```bash
gcloud auth login
```

Both projects should be listed:

```bash
gcloud projects list
```
