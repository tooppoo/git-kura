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

### Supported platforms

| OS    | Architecture       |
|-------|--------------------|
| Linux | x86\_64 / amd64   |
| Linux | arm64 / aarch64   |
| macOS | x86\_64           |
| macOS | arm64 (Apple Silicon) |

### Verification

The installer always verifies the SHA-256 checksum of the downloaded archive against `checksums.txt` from the same release. A mismatch causes the installer to abort before touching `~/.local/bin`.

If `cosign` is on your `PATH`, the installer additionally verifies the `checksums.txt` signature bundle (`checksums.txt.sigstore.json`) published with each release. You can make this check mandatory with `--require-signature`.

---

## Manual download and verification

### Linux and macOS

1. Choose your archive from [GitHub Releases](https://github.com/tooppoo/git-kura/releases):

   ```
   git-kura_<VERSION>_Linux_x86_64.tar.gz
   git-kura_<VERSION>_Linux_arm64.tar.gz
   git-kura_<VERSION>_Darwin_x86_64.tar.gz
   git-kura_<VERSION>_Darwin_arm64.tar.gz
   ```

2. Download the archive and `checksums.txt`:

   ```sh
   VERSION=v0.0.2
   OS=Linux          # or Darwin
   ARCH=x86_64       # or arm64
   ARCHIVE="git-kura_${VERSION}_${OS}_${ARCH}.tar.gz"

   curl -fLO "https://github.com/tooppoo/git-kura/releases/download/${VERSION}/${ARCHIVE}"
   curl -fLO "https://github.com/tooppoo/git-kura/releases/download/${VERSION}/checksums.txt"
   ```

3. Verify the checksum:

   ```sh
   # Linux
   grep " ${ARCHIVE}$" checksums.txt | sha256sum -c -

   # macOS (if sha256sum is unavailable)
   grep " ${ARCHIVE}$" checksums.txt | shasum -a 256 -c -
   ```

4. Extract and install:

   ```sh
   tar -xzf "${ARCHIVE}"
   chmod +x git-kura
   mv git-kura ~/.local/bin/git-kura
   ```

5. Verify:

   ```sh
   git kura -h
   ```

### Optional: cosign signature verification

Each release publishes `checksums.txt.sigstore.json`. If you have [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) installed:

```sh
curl -fLO "https://github.com/tooppoo/git-kura/releases/download/${VERSION}/checksums.txt.sigstore.json"

cosign verify-blob \
    --bundle checksums.txt.sigstore.json \
    --certificate-identity-regexp "https://github.com/tooppoo/git-kura/.*" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    checksums.txt
```

### Windows

1. Download the archive and `checksums.txt`:

   ```powershell
   $Version = "v0.0.2"
   $Arch    = "x86_64"    # or arm64
   $Archive = "git-kura_${Version}_Windows_${Arch}.zip"

   Invoke-WebRequest "https://github.com/tooppoo/git-kura/releases/download/$Version/$Archive" -OutFile $Archive
   Invoke-WebRequest "https://github.com/tooppoo/git-kura/releases/download/$Version/checksums.txt" -OutFile checksums.txt
   ```

2. Verify the checksum:

   ```powershell
   $Expected = ((Select-String -Path checksums.txt -Pattern " $Archive$").Line -split '\s+')[0].ToLower()
   $Actual   = (Get-FileHash $Archive -Algorithm SHA256).Hash.ToLower()
   if ($Actual -ne $Expected) { throw "checksum mismatch: expected $Expected, got $Actual" }
   ```

3. Extract and install:

   ```powershell
   Expand-Archive $Archive -DestinationPath . -Force
   $InstallDir = Join-Path $HOME "bin"
   New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
   Copy-Item .\git-kura.exe (Join-Path $InstallDir "git-kura.exe")
   $env:Path = "$InstallDir;$env:Path"
   ```

4. Verify:

   ```powershell
   git kura -h
   ```

---

## Build from source

Requirements: Go toolchain, Git

```sh
git clone https://github.com/tooppoo/git-kura.git
cd git-kura
make build
```

This produces `./bin/git-kura`. Place it somewhere on `PATH`:

```sh
cp ./bin/git-kura ~/.local/bin/git-kura
```
