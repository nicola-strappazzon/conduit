# Conduit

[![Test](https://github.com/nicola-strappazzon/conduit/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/nicola-strappazzon/conduit/actions/workflows/test.yml)
[![Latest Release](https://img.shields.io/github/release/nicola-strappazzon/conduit)](https://github.com/nicola-strappazzon/conduit/releases)

A CLI that simplifies AWS SSM port forwarding.

Conduit opens and maintains port-forwarding sessions through AWS Systems Manager. It can complete an SSO device login in your browser and reconnect automatically when a session ends.

[Requirements](#requirements) · [Installation](#installation) · [Usage](#usage)

## How it works

Conduit uses an SSM-managed bastion to reach services in a private network:

```text
Your machine:3306 → SSM session → bastion:3306 → socat → RDS:3306
```

The bastion must be managed by AWS Systems Manager, be able to reach the RDS instance, and have `socat` installed. Before starting Conduit, run this on the bastion (replace the hostname and port as needed):

```bash
sudo nohup socat TCP-LISTEN:3306,fork,reuseaddr TCP:example.cxvub4jf47su.eu-central-1.rds.amazonaws.com:3306 >/tmp/socat-3306.log 2>&1 &
```

This starts a listener on the bastion's port `3306` and forwards its traffic to the RDS endpoint. Its output is written to `/tmp/socat-3306.log`.

## Requirements

- An AWS profile configured for Systems Manager, with permission to start SSM sessions.
- The [Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html) available in your `PATH`.
- An SSM-managed bastion that can reach the target service, with `socat` installed.
- Go, using the version declared in `go.mod`, when building from source.

## Installation

### macOS

Install Conduit from the Homebrew tap:

```bash
brew install nicola-strappazzon/tap/conduit
xattr -d com.apple.quarantine /opt/homebrew/bin/conduit
```

### From source

For local development or platforms without a Homebrew release:

```bash
task build
```

Move `conduit` to a directory in your `PATH` if you want to use it globally.

## Usage

Run the command without flags to see all available options:

```bash
conduit
```

After starting `socat` on the bastion, forward the local port through it:

```bash
conduit \
  --profile my-profile \
  --target i-bastion \
  --remote-port 3306 \
  --local-port 3306
```

Alternatively, Conduit can forward directly to a host reachable from the bastion without `socat`:

```bash
conduit \
  --profile my-profile \
  --target i-0123456789abcdef0 \
  --document AWS-StartPortForwardingSessionToRemoteHost \
  --remote-host database.internal \
  --remote-port 3306 \
  --local-port 3306
```

Use `--reconnect=false` to exit after the session ends instead of reconnecting.

Press `Ctrl+C` to stop Conduit and close the local tunnel. AWS can retain the Session Manager record until its configured timeout.

## License

Conduit is licensed under the [GNU General Public License v3.0](LICENSE).
