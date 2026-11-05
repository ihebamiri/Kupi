# Kupi

Kupi (Kubernetes Pilot) is a free, open-source desktop app for managing Kubernetes clusters, from a laptop to production. It puts cluster resources, logs, metrics, images, secrets and Helm releases in one window, with a visual map of how everything connects.

**Status:** early development. Not ready for production clusters yet.

## Why Kupi

- **One interface for beginners and experts.** Real Kubernetes names, plain-language explanations, and YAML always one click away.
- **Safe by design.** Every change to a cluster passes one checkpoint: dry run, diff, confirmation and a tamper-evident audit log. Production clusters open read-only.
- **Local and production.** Defaults adapt to the cluster: fast and direct on k3d, careful and explicit on production.
- **Light.** Go and the system web view, no bundled browser.
- **Private.** No telemetry, no accounts, no server.

## What it does

- Browse every resource type (custom resources included), with logs, a shell, events and live metrics.
- Create and edit with forms generated from the cluster's own schema, kept in sync with a YAML editor.
- A live map of how Ingresses, Services, workloads, config and storage connect, with health and security overlays.
- Port forwards that reconnect on their own, on 127.0.0.1 only.
- Local images and k3d clusters: pull, load into a cluster, deploy; create, start, stop; laptop mode caps a cluster's memory and CPU and stops it when idle.
- Secrets without leaks: values stay hidden until you reveal one key, copies clear themselves, and edits send only the keys you change. A local vault keeps credentials encrypted on your computer.
- History of what changed and which tool changed it, and an incident view: what's failing, and what changed right before.
- Helm releases (values, upgrade with a diff, rollback, uninstall) and Argo CD and Flux status.

## Platforms

Linux first, then Windows.

## Development

Requirements: Go 1.26+, Node 22, Wails v3 CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`), Docker and k3d for integration tests. On Linux, Wails also needs `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`.

```bash
wails3 dev
```

```bash
go test ./...
```

```bash
cd frontend && npm test
```

Integration tests run against a local k3d cluster:

```bash
wails3 task cluster:up
```

```bash
wails3 task test:integration
```

If Go runs out of memory while building, add `-p 2` to `go build` and `go test`.

See [docs/TESTING.md](docs/TESTING.md) for the hands-on checklist, [docs/PROGRESS.md](docs/PROGRESS.md) for what's done, and [CLAUDE.md](CLAUDE.md) for the architecture and security rules.

## License

Apache 2.0. See [LICENSE](LICENSE).
