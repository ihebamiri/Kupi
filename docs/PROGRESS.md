# Progress

Scope of the current build: v0.1 + the v0.2 Observe canvas and v0.2 features, per the design doc.

## Milestones

- [x] 1. Foundation: kubeconfig loading, trust prompt, profiles, sessions, Guard, audit log, security headers, architecture tests. Reviewed and committed.
- [x] 2. Core views: resource browser (all types via discovery), logs, shell, events.
- [ ] 3. Forms with YAML sync (hand-designed forms for the main types, generated forms for the rest).
- [ ] 4. Live metrics and port-forward manager.
- [ ] 5. Observe canvas.
- [ ] 6. Images and k3d local clusters.
- [ ] 7. Secrets and local vault.
- [ ] 8. Helm, Argo CD / Flux awareness, resource history, incident view, laptop mode.
- [ ] 9. Final checks before maintainer testing.

## Milestone 1 status

Done and verified:

- Go unit tests for every foundation package, plus `internal/archtest` (Guard-only writes, client-go confinement, no shell, no unsafe HTML in the frontend).
- Integration test `internal/guard/integration_test.go` against k3d: create, update with diff, Secret redaction, stale-plan refusal, delete, audit chain, production read-only.
- UI checked end to end in a browser through Wails server mode (`go build -tags server`): connect, preview, apply, production read-only, editing on with reason, change refused without a reason, audit chain verified.
- Production build checked with the content security policy active: no console errors, backend calls work.

Not yet verified:

- The native desktop window (WebView2 / WebKitGTK); only the browser path was exercised.
- The Linux build: needs Ubuntu in WSL2 on the maintainer's PC.
- The CI workflow has not run yet (no remote).

Follow-ups:

- Pin GitHub Actions to commit SHAs before the repository goes public.
- Add a CodeQL workflow.
- Kupi should explain the k3d-on-Windows `host.docker.internal` problem when a connection to such a context times out.

## Milestone 2 status

Done and verified (unit tests, k3d integration tests, and clicked through in server mode):

- Resource browser for every listable type, found through discovery and grouped by category (custom resources included). Lists use the API server's own table format, so columns match `kubectl get`, including CRD printer columns.
- Live updates: a metadata-only watch per open list, throttled to one reload per 750 ms. Secret contents never travel over the watch.
- Detail panel: overview, YAML (Secrets redacted, `last-applied-configuration` stripped), events, logs, shell; scale, restart and delete through the Guard.
- Logs: one pod or every pod of a workload, merged and prefixed, follow, previous, timestamps, filter, pause; 50,000-line cap per view.
- Shell: xterm.js terminal over websocket exec (SPDY fallback). Goes through the Guard: RBAC `pods/exec` check, reason on production, audit entry before the shell opens. Not blocked by read-only mode.
- Command palette (Ctrl+K), namespace picker (production starts scoped to one namespace), "Copy as kubectl" on lists and actions.
- The terminal library is code-split: main bundle 318 KB (101 KB gzipped); xterm loads only when a shell opens.

Known limits:

- A workload's log view follows the pods that existed when it opened; new pods appear after reopening it.
- Lists reload whole on change; informer-based diffs are a later optimisation for very large clusters.
- Editing Secrets waits for milestone 7; their YAML view is read-only.

Test fixtures: `docs/testdata/demo-workloads.yaml` (namespace `kupi-demo`: a 2-copy log ticker, a crash-looping pod, a ConfigMap and a Secret).

## Environment

- Go 1.27, Wails v3.1.0, client-go v0.37 (supports Kubernetes 1.34 to 1.37), k3d 5.9, Node 22.
- Developed on Windows; the Linux build is tested in WSL2 Ubuntu.
- k3d on Windows: create clusters with `--api-port 127.0.0.1:<port>`; the default `host.docker.internal` address can resolve to a stale IP.
- UI testing without a native window: build with `-tags server` and run with `WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=<port> KUPI_CONFIG_DIR=<temp dir>`. Never ship a server-mode build.

## Log

- 2026-11-05: project generated from the Wails v3 React template; macOS, iOS and Android build files removed.
- 2026-11-05: milestone 1 foundation implemented and verified.
- 2026-11-05: milestone 2 core views implemented and verified.
