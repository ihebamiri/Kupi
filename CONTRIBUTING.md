# Contributing to Kupi

Thanks for helping. Kupi manages real clusters, often production ones, so the bar for changes that touch a cluster or a secret is high. This page explains how the code is organised and the rules every change keeps.

## Ways to help

- **Report a bug** with the [bug report form](https://github.com/ihebamiri/Kupi/issues/new?template=bug_report.yml). Include your OS, the Kupi version (bottom of the sidebar), the cluster's Kubernetes version and the steps to reproduce.
- **Suggest a feature** with the [feature request form](https://github.com/ihebamiri/Kupi/issues/new?template=feature_request.yml).
- **Report a security problem privately:** see [SECURITY.md](SECURITY.md). Please don't open a public issue for it.
- **Send a pull request.** For anything bigger than a small fix, open an issue first so we can agree on the approach.

## Getting started

You need Go 1.26 or newer, Node 22 and the Wails v3 CLI; on Linux also `libgtk-4-dev` and `libwebkitgtk-6.0-dev`. Docker and k3d are needed for the integration tests.

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

```bash
wails3 dev
```

Before you open a pull request:

```bash
go test ./...
```

```bash
cd frontend && npm test && npm run build
```

`go test ./...` includes the architecture tests in `internal/archtest`, which enforce the rules below. Integration tests run against a local k3d cluster (`wails3 task cluster:up`, then `wails3 task test:integration`).

After changing anything in `internal/bridge`, regenerate the TypeScript bindings and commit them; never edit `frontend/bindings` by hand:

```bash
wails3 generate bindings -ts -i
```

## How the code is organised

| Where | What |
|---|---|
| `internal/bridge` | The only code the UI can call. Thin: validate input, call a package, return plain data. |
| `internal/guard` | The single checkpoint for every cluster change: profile rules, permission check, dry run and diff, confirmation, audit. |
| `internal/kube` | Cluster sessions. Exposes a read-only `Reader` to everyone and a `Mutator` to the Guard only. |
| `internal/…` | One package per feature: secrets, vault, history, gitops, helmrel, terminal, settings and more. |
| `frontend/src` | React and TypeScript: pages, shared components, state, and pure helpers with tests. |

[How Kupi works](docs/ARCHITECTURE.md) explains each part from the ground up.

## Rules every change keeps

1. **Every change to a cluster goes through `internal/guard`.** Only the Guard may call `Mutator()`. Feature code gets a `kube.Reader`. (Enforced by `archtest`.)
2. **Only `internal/kube` talks to the API server** through client-go's dynamic, typed, REST, exec and port-forward clients. (Enforced.)
3. **No shells.** Start programs with an absolute path and an argument list (`internal/procutil`); never `sh -c`, `cmd /c` or `powershell -Command`. The opt-in integrated terminal in `internal/terminal` is the single, bounded exception. (Enforced.)
4. **Secret values never leak:** not into logs, errors, diffs or the UI, unless the user explicitly reveals one. Use `internal/redact`.
5. **Every bridge method validates every input in Go:** names, namespaces, types, sizes. The frontend is untrusted.
6. **No catch-all functions:** nothing like "run any command", "send any request" or "read any file".
7. **Frontend:** no `dangerouslySetInnerHTML`, `eval` or `new Function`, no remote scripts, styles or fonts. Treat all cluster data as hostile and show it as text. (Enforced.)
8. **Port-forwards bind to `127.0.0.1` only.**
9. **Files Kupi writes** go in its config dir with user-only permissions, written atomically (`internal/appdir`).
10. **Unknown clusters are treated as production.**
11. **New dependencies** need a good reason and a maintainer's agreement. Prefer the standard library.

## Style

- Go: `gofmt`; wrap errors with `fmt.Errorf("…: %w", err)`; long-running calls take a `context.Context`; no goroutine without a way to stop it.
- Server-side apply with the field manager `kupi`; never take over fields from other tools silently.
- Tests sit next to the code (`*_test.go`); integration tests use the `integration` build tag.
- UI text: sentence case, plain language, real Kubernetes names.
- Commits: one topic each, with a subject line in the imperative ("Add …", "Fix …").

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
