# Installation

## curl installer (Linux and macOS)

The quickest way to install `git-kura` on Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh
```

The installer detects your OS and CPU architecture, downloads the matching release archive from GitHub, verifies the SHA-256 checksum, and installs the binary as `git-kura` into `~/.local/bin`.

### Options

Install a specific version:

```sh
curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh -s -- --version v0.0.2
```

Install to a custom directory:

```sh
curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh -s -- --install-dir "$HOME/bin"
```

Require cosign signature verification (fails if `cosign` is not installed or the signature bundle is unavailable):

```sh
curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh -s -- --require-signature
```

### PATH

The installer prints a reminder if `~/.local/bin` is not on your `PATH`. Add it to your shell profile if needed:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Git recognises `git-kura` as the external subcommand `git kura` automatically once the binary is on `PATH`.

### Uninstall

The curl installer does not write package-manager metadata. To uninstall a default curl installation, remove the installed binary:

```sh
rm -f "$HOME/.local/bin/git-kura"
```

If you installed with `--install-dir`, remove `git-kura` from that directory instead:

```sh
rm -f "$HOME/bin/git-kura"
```

This removes only the `git-kura` executable installed by the curl installer. Repository-local state and tool components installed later with `git kura tools install ...` are managed separately.

### Verification

The installer always verifies the SHA-256 checksum of the downloaded archive against `checksums.txt` from the same release. A mismatch causes the installer to abort before touching `~/.local/bin`.

If `cosign` is on your `PATH`, the installer additionally verifies the `checksums.txt` signature bundle (`checksums.txt.sigstore.json`) published with each release. You can make this check mandatory with `--require-signature`.

## Manual archive install

Download and extract a release archive, then place `git-kura` somewhere on `PATH`:

```sh
cp ./git-kura ~/.local/bin/git-kura
```

## Dev Container

`git-kura` is also provided as feature for [devcontainer](https://code.visualstudio.com/docs/devcontainers/containers).

```json
"features": {
    "ghcr.io/tooppoo/catalog-devcontainer-features/git-kura:0": {}
}
```

detail: [tooppoo/catalog-devcontainer-features : src/git-kura](https://github.com/tooppoo/catalog-devcontainer-features/tree/main/src/git-kura)

## Homebrew (Linux and macOS)

```
brew tap tooppoo/tap-catalog
brew install tooppoo/tap-catalog/git-kura
```

detail: <https://github.com/tooppoo/catalog-devcontainer-features>

## Scoop (Windows)

On Windows, `git-kura` can be installed from the Philomagi Scoop bucket:

```powershell
scoop bucket add philomagi https://github.com/tooppoo/catalog-scoop-bucket
scoop install git-kura
```

detail: <https://github.com/tooppoo/catalog-scoop-bucket>

## Supported platforms

| OS    | Architecture       |
|-------|--------------------|
| Linux | x86\_64 / amd64   |
| Linux | arm64 / aarch64   |
| macOS | x86\_64           |
| macOS | arm64 (Apple Silicon) |
| Windows | x86\_64 |
| Windows | arm64 |
