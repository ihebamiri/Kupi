# Kupi

Kupi (Kubernetes Pilot) is a free, open-source desktop app for managing Kubernetes clusters, from a laptop to production. It puts cluster resources, logs, metrics, images and secrets in one window, with a visual map of how everything connects.

**Status:** early development. Not ready for production clusters yet.

## Why Kupi

- **One interface for beginners and experts.** Real Kubernetes names, plain-language explanations, and YAML always one click away.
- **Safe by design.** Every change to a cluster passes one checkpoint: dry run, diff, confirmation and a tamper-evident audit log. Production clusters open read-only.
- **Local and production.** Defaults adapt to the cluster: fast and direct on k3d, careful and explicit on production.
- **Light.** Go and the system web view, no bundled browser.
- **Private.** No telemetry, no accounts, no server.

## Platforms

Linux first, then Windows.

## Development

Requirements: Go 1.26+, Node 20+, Wails v3 CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`), Docker and k3d for integration tests. On Linux, Wails also needs `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`.

```bash
wails3 dev
```

```bash
go test ./...
```

## License

Apache 2.0. See [LICENSE](LICENSE).
