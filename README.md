# Conduit

[![Test](https://github.com/nicola-strappazzon/conduit/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/nicola-strappazzon/conduit/actions/workflows/test.yml)
[![Latest Release](https://img.shields.io/github/release/nicola-strappazzon/conduit)](https://github.com/nicola-strappazzon/conduit/releases)

[Requirements](#requirements) · [Installation](#installation) · [Usage](#usage)

Conduit is a CLI for AWS SSM port forwarding. It opens the SSO login page when needed and reconnects when a session ends.

![Demo](assets/demo.gif)

## Advantages

- Reach private services with one command.
- No public SSH port or SSH keys.
- Handle AWS SSO in your browser.
- Reconnect automatically.

## How it works

Conduit sends traffic through an SSM-managed bastion to a private service:

```mermaid
flowchart LR
    local["Your machine<br/>localhost:3306"]
    bastion["Bastion<br/>:3306"]
    rds["RDS<br/>:3306"]

    local -->|SSM protocol| bastion
    bastion -->|socat| rds
```

The bastion needs SSM, access to the RDS instance, and `socat`. Run this on the bastion before using Conduit. Change the hostname and port as needed:

```bash
sudo nohup socat TCP-LISTEN:3306,fork,reuseaddr TCP:example.cxvub4jf47su.eu-central-1.rds.amazonaws.com:3306 >/tmp/socat-3306.log 2>&1 &
```

This listens on port `3306` and sends traffic to the RDS endpoint. Logs go to `/tmp/socat-3306.log`.

## Requirements

- An AWS profile allowed to start SSM sessions.
- The [Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html) in your `PATH`.
- An SSM-managed bastion that can reach the service.
- `socat` on the bastion when using the first example below.
- Go, using the version in `go.mod`, when building from source.

## Installation

### macOS

Install with Homebrew:

```bash
brew install nicola-strappazzon/tap/conduit
xattr -d com.apple.quarantine /opt/homebrew/bin/conduit
```

Run the second command only if macOS blocks the app.

### From source

Build from source:

```bash
task build
```

Move `conduit` to a directory in your `PATH` to run it from anywhere.

## Usage

Run without flags to see all options:

```bash
conduit
```

After starting `socat` on the bastion, open the tunnel:

```bash
conduit \
  --profile my-profile \
  --target i-bastion \
  --remote-port 3306 \
  --local-port 3306
```

Or connect directly to a host that the bastion can reach. This does not need `socat`:

```bash
conduit \
  --profile my-profile \
  --target i-0123456789abcdef0 \
  --document AWS-StartPortForwardingSessionToRemoteHost \
  --remote-host database.internal \
  --remote-port 3306 \
  --local-port 3306
```

Use `--reconnect=false` to stop when the session ends.

Press `Ctrl+C` to close the local tunnel. AWS keeps the session record until its timeout.

## License

Licensed under the [GNU General Public License v3.0](LICENSE).
