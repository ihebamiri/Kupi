# Testing Kupi

- **On Windows, by hand:** [TESTING-WINDOWS.md](TESTING-WINDOWS.md): install the prerequisites, create a test cluster with sample data, then check every feature step by step.
- **On Linux, by hand:** [TESTING-LINUX.md](TESTING-LINUX.md): which distributions work, installing the packages, and the checks that differ from Windows.

## Automated tests

For developers, from the repository root:

```bash
go test ./...
```

```bash
cd frontend && npm test
```

Integration tests run against the local k3d cluster `kupi-test`:

```bash
wails3 task cluster:up
```

```bash
wails3 task test:integration
```

`KUPI_DEMO_RELEASE=1 go test -tags integration -run TestInstallDemoRelease ./internal/kube/` installs the demo Helm release from `docs/testdata/charts/demo-web` without the Helm CLI.

If Go runs out of memory while building (the Helm SDK is large), add `-p 2` to `go build` and `go test`.
