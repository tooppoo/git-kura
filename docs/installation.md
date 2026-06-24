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


### Verification

The installer always verifies the SHA-256 checksum of the downloaded archive against `checksums.txt` from the same release. A mismatch causes the installer to abort before touching `~/.local/bin`.

If `cosign` is on your `PATH`, the installer additionally verifies the `checksums.txt` signature bundle (`checksums.txt.sigstore.json`) published with each release. You can make this check mandatory with `--require-signature`.
ura`. Place it somewhere on `PATH`:

```sh
cp ./bin/git-kura ~/.local/bin/git-kura
```

## Scoop (Windows)

On Windows, `git-kura` can be installed from the Philomagi Scoop bucket:

```powershell
scoop bucket add philomagi https://github.com/tooppoo/catalog-scoop-bucket
scoop install git-kura
````

After installation, verify that Git external subcommand are available:

```powershell
git kura -v
```

To uninstall:

```powershell
scoop uninstall git-kura
```

## Supported platforms

| OS    | Architecture       |
|-------|--------------------|
| Linux | x86\_64 / amd64   |
| Linux | arm64 / aarch64   |
| macOS | x86\_64           |
| macOS | arm64 (Apple Silicon) |
| Windows | x86\_64 |
| Windows | arm64 |
