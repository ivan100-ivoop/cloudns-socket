# ClouDNS Socket Gateway

A small Go service that listens on a TCP socket, accepts a domain name, resolves the correct provider by TLD, checks availability via the provider API, and returns a normalized status string.

## Project goals

- lightweight socket-based domain availability checker
- plugin-like provider system driven by YAML
- easy local development and ops deployment
- simple cross-platform builds for Linux/macOS/Windows

## Repository

- GitHub: https://github.com/ivan100-ivoop/cloudns-socket

## Tech stack

- Go 1.22+
- YAML-driven provider configuration
- standard library networking and HTTP client
- no external dependencies aside from `gopkg.in/yaml.v3`

## Structure

- `cmd/cloudns-socket` — CLI entrypoint
- `gateway/` — TCP server, config validation, request handling
- `provider/` — provider registry, template rendering, HTTP logic, matching rules
- `config/` — runtime config and provider definitions

## Prerequisites

This project was developed and tested in a Windows environment, but the Go application is cross-platform and can also be built and run on Linux and macOS.

### Install Go on Windows

1. Download the Windows installer from the official Go website:

  https://go.dev/dl/

2. Run the `.msi` installer and keep the default installation options.
3. Restart PowerShell or open a new terminal.
4. Verify the installation:

### Install Go on Linux

Download the Linux archive from the official Go website, extract it to `/usr/local`, and add Go to your `PATH`:

```bash
curl -LO https://go.dev/dl/go1.22.12.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.12.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile
go version
```

Replace the archive name with the current version for your Linux architecture. Some distributions also provide Go through their package manager, but the official download is recommended when you need a specific Go version.

### Verify Go
```powershell
go version
```

Go should print the installed version, for example `go version go1.22.x .../...`.
On Linux or macOS, run the same command from a terminal. Use Go 1.22 or newer.

## Install and setup

From the project root:

```bash
go mod download
```

Then update the config files before running anything:

- `config/gateway.yml`
- `config/providers/cloudns.yml`

Important:

- never commit real CloudNS credentials
- use placeholders or secret storage in your own environment
- keep `allowed_ips` restricted to trusted source addresses only

Example minimal config:

```yaml
server:
  host: "0.0.0.0"
  port: 43
  read_timeout: 10s
  write_timeout: 10s
  idle_timeout: 15s
  max_request_size: 512
  error_response: "DOMAIN_GATEWAY_ERROR"
  allowed_ips:
    - "127.0.0.1"

providers_path: "providers"
```

Provider config example:

```yaml
variables:
  auth_id: "REPLACE_WITH_CLOUDNS_AUTH_ID"
  auth_password: "REPLACE_WITH_CLOUDNS_PASSWORD"
```

## Local run

Start the gateway:

```bash
go run ./cmd/cloudns-socket -p ./config
```

Run a one-off availability check:

```bash
go run ./cmd/cloudns-socket -p ./config check example.info
```

The service expects a raw domain line, for example:

```text
example.info
```

and responds with a single status line such as:

```text
CLOUDNS_AVAILABLE
```

## Build

Standard local build:

```bash
go build ./...
```

Single binary build for the current machine:

```bash
go build -o bin/cloudns-socket ./cmd/cloudns-socket
```

## Cross-compile

Cross-compile for Linux AMD64:

```bash
GOOS=linux GOARCH=amd64 go build -o bin/cloudns-socket-linux-amd64 ./cmd/cloudns-socket
```

Cross-compile for Linux ARM64:

```bash
GOOS=linux GOARCH=arm64 go build -o bin/cloudns-socket-linux-arm64 ./cmd/cloudns-socket
```

Cross-compile for macOS AMD64:

```bash
GOOS=darwin GOARCH=amd64 go build -o bin/cloudns-socket-darwin-amd64 ./cmd/cloudns-socket
```

Cross-compile for macOS ARM64:

```bash
GOOS=darwin GOARCH=arm64 go build -o bin/cloudns-socket-darwin-arm64 ./cmd/cloudns-socket
```

Cross-compile for Windows AMD64:

```bash
GOOS=windows GOARCH=amd64 go build -o bin/cloudns-socket-windows-amd64.exe ./cmd/cloudns-socket
```

If you need a fully reproducible release pipeline, keep the same command in CI and upload the generated binaries from `bin/`.

## Tests

Run the whole suite:

```bash
go test ./...
```

Run a focused package test:

```bash
go test ./provider/... 
go test ./gateway/...
```

For local validation before release:

```bash
go test ./... && go build ./...
```

## Notes for ops / deployment

- restrict access via `allowed_ips`
- use a non-root service user where possible
- keep provider creds in a secret manager or environment-specific config
- a debug log file is available when `debug: true` is enabled in the provider config
- keep the socket port behind firewall rules if exposed externally

## Useful links

- Go homepage: https://go.dev/
- Go downloads: https://go.dev/dl/
- Go documentation: https://go.dev/doc/
- CloudNS homepage: https://www.cloudns.net/
- CloudNS API documentation: https://www.cloudns.net/wiki/
- Project repository: https://github.com/ivan100-ivoop/cloudns-socket

## License

This project is provided as-is for educational and operational use.
