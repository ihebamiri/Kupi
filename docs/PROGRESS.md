# Progress

Scope of the current build: v0.1 + the v0.2 Observe canvas and v0.2 features, per the design doc.

## Milestones

- [x] 1. Foundation: kubeconfig loading, trust prompt, profiles, sessions, Guard, audit log, security headers, architecture tests. **Waiting for maintainer review.**
- [ ] 2. Core views: resource browser (all types via discovery), logs, shell, events.
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

## Environment

- Go 1.27, Wails v3.1.0, client-go v0.37 (supports Kubernetes 1.34 to 1.37), k3d 5.9, Node 22.
- Developed on Windows; the Linux build is tested in WSL2 Ubuntu.
- k3d on Windows: create clusters with `--api-port 127.0.0.1:<port>`; the default `host.docker.internal` address can resolve to a stale IP.
- UI testing without a native window: build with `-tags server` and run with `WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=<port> KUPI_CONFIG_DIR=<temp dir>`. Never ship a server-mode build.

## Log

- 2026-11-05: project generated from the Wails v3 React template; macOS, iOS and Android build files removed.
- 2026-11-05: milestone 1 foundation implemented and verified.
