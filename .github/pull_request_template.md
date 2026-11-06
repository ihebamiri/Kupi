## What this changes

<!-- What and why, in a few sentences. Link the issue: "Fixes #123". -->

## How it was tested

<!-- Unit tests, integration tests against k3d, or steps you clicked through in the app. -->

## Checklist

- [ ] `go test ./...` and `cd frontend && npm test` pass
- [ ] Cluster changes go through `internal/guard`; no new shell or catch-all function
- [ ] No Secret values in logs, errors, diffs or the UI
- [ ] New bridge inputs are validated in Go
- [ ] Bindings regenerated if `internal/bridge` changed (`wails3 generate bindings -ts -i`)
- [ ] Docs updated if behaviour changed (user guide, architecture)
