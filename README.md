# bm

Binary manager (`bm`) keeps a set of CLI binaries in sync by downloading them from their release URLs into local directories. Define what you want once in a YAML file, run `bm sync` and every binary is up-to-date. Intended for use together with renovate to automate updates.

## Install

Pre built binaries are available on GitHub Releases. This tool is intended to be updated with itself, so you can run `bm sync` to get the latest version.

## Usage

```text
bm sync [flags]

Flags:
  -c, --config string     Path to the configuration file (default "releases.yaml")
      --only strings      Process only specified release IDs, comma-separated
      --dry-run           Show what would be done without making changes
      --log-level string  Log level: trace, debug, info, warn, error (default "info")
```

### Examples

```bash
# Sync everything defined in releases.yaml
bm sync

# Specify a custom config file
bm sync --config my-releases.yaml

# Preview without downloading
bm sync --dry-run

# Only sync helm and kubectl
bm sync --only helm,kubectl

# Verbose output
bm sync --log-level debug
```

Environment variables are supported with the `BM_` prefix for root flags and `BM_<SUBCOMMAND>_` for subcommand flags. E.g. `BM_SYNC_CONFIG` for the `config` flag of the `sync` subcommand.

## Configuration

Configuration is defined in a YAML file (default `releases.yaml`).

```yaml
releases:
  - id: helm
    src: https://get.helm.sh/helm-v4.1.4-linux-amd64.tar.gz
    dst: /usr/local/bin
    regex: "helm-v(?P<Version>[^-]+)-linux-amd64"
    unpack: tar.gz
    assets:
      - src: linux-amd64/helm
        dst: helm
    integrity:
      key: https://keys.openpgp.org/vks/v1/by-fingerprint/BF888333D96A1C18E2682AAED79D67C9EC016739
      signature: https://get.helm.sh/helm-v{{.Version}}-linux-amd64.tar.gz.asc

  - id: kubectl
    src: https://dl.k8s.io/release/v1.35.0/bin/linux/amd64/kubectl
    dst: /usr/local/bin
    assets:
      - src: kubectl
        dst: kubectl
```

### Fields

Full specification is available in the [Golang types](internal/release/release.go).

| Field       | Required | Description                                                        |
| ----------- | -------- | ------------------------------------------------------------------ |
| `id`        | yes      | Unique name used in logs and `--only` filtering                    |
| `src`       | yes      | Download URL (may contain `{{.VarName}}` template syntax)          |
| `dst`       | yes      | Destination directory (must already exist)                         |
| `unpack`    | no       | Archive format to extract: `tar.gz`, `tar`, or `zip`               |
| `assets`    | no       | Files to copy from the work directory to `dst`                     |
| `regex`     | no       | Named-capture regex applied to `src` to extract template variables |
| `integrity` | no       | Verification block (see below)                                     |

### Template Variables

The `regex` field accepts a Go regular expression with named capture groups. Captured values become template variables available in `src`, `assets[].src`, `assets[].dst`, `integrity.key`, `integrity.signature` and `integrity.bundle`.

```yaml
- id: crane
  src: https://github.com/google/go-containerregistry/releases/download/v0.21.5/go-containerregistry_Linux_x86_64.tar.gz
  regex: "go-containerregistry_Linux_x86_64"
  dst: /usr/local/bin
  unpack: tar.gz
  assets:
    - src: crane
      dst: crane
```

For a release where the version appears in both the archive URL and the signature URL:

```yaml
- id: helm
  src: https://get.helm.sh/helm-v4.1.4-linux-amd64.tar.gz
  regex: "helm-v(?P<Version>[^-]+)-linux-amd64"
  integrity:
    signature: https://get.helm.sh/helm-v{{.Version}}-linux-amd64.tar.gz.asc
```

### Integrity Verification

Four algorithms are supported. If verification fails the release is aborted and nothing is written to the destination.

#### PGP

```yaml
integrity:
  algorithm: pgp
  key: <url> # URL to a PEM or ASCII-armored public key
  signature: <url> # URL to a detached .asc signature
```

#### Cosign (key-based)

```yaml
integrity:
  algorithm: cosign
  key: <url> # URL to a PEM public key, PEM X.509 certificate, or base64-encoded version of either
  signature: <url> # URL to a base64-encoded detached signature (.sig file)
```

#### Sigstore bundle

```yaml
integrity:
  algorithm: bundle
  bundle: <url> # URL to the .bundle JSON file
```

#### SHA256 checksum

```yaml
integrity:
  algorithm: sha256
  signature: <url> # URL to the checksum file
```

The checksum file may be a bare 64-character hex digest or a `sha256sum(1)`-format file with one or more `<hash>  <filename>` lines. When the file has multiple entries the correct hash is selected by matching the downloaded filename.

## How It Works

For each release, `bm sync`:

1. Evaluates `regex` against `src` and expands any `{{.Var}}` templates
2. Downloads `src` to an isolated temporary directory (auto-cleaned on exit)
3. Verifies integrity if an `integrity` block is present (PGP, cosign, Sigstore bundle, or SHA256)
4. Extracts the archive if `unpack` is set
5. Copies each `asset` from the temporary directory to `dst`, ensuring the execute bit is set
