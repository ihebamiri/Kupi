# Progress

Scope of the current build: v0.1 + the v0.2 Observe canvas and v0.2 features, per the design doc.

## Milestones

- [x] 1. Foundation: kubeconfig loading, trust prompt, profiles, sessions, Guard, audit log, security headers, architecture tests. Reviewed and committed.
- [x] 2. Core views: resource browser (all types via discovery), logs, shell, events.
- [x] 3. Forms with YAML sync (hand-designed forms for the main types, generated forms for the rest).
- [x] 4. Live metrics and port-forward manager.
- [x] 5. Observe canvas.
- [x] 6. Images and k3d local clusters.
- [x] 7. Secrets and local vault.
- [x] 8. Helm, Argo CD / Flux awareness, resource history, incident view, laptop mode.
- [x] 9. Final checks before maintainer testing.
- [x] 10. Maintainer feedback, round 1: multi-object manifests and file upload, form suggestions and sliders, pods on the map, Settings, the integrated terminal, logo and visual polish.

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

- ~~Pin GitHub Actions to commit SHAs~~ done in milestone 9.
- ~~Add a CodeQL workflow~~ done in milestone 9.
- ~~Explain the k3d-on-Windows `host.docker.internal` timeout~~ done in milestone 9.

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

## Milestone 3 status

Done and verified:

- Forms generated from each cluster's OpenAPI v3 schema (`internal/schema`), so custom resources get forms too. Deployment form tree is 327 KB; schemas are cached for 10 minutes.
- Curated "common fields first" layouts and starter templates for Deployment, Service, ConfigMap, Namespace, Job, CronJob, PVC (Ingress and HTTPRoute layouts without templates). Workload templates start secure: non-root, read-only root filesystem, no privilege escalation, capabilities dropped, seccomp, resource limits. Verified a template Deployment runs Ready on k3d.
- Form and CodeMirror YAML editor stay in sync (YAML conversion done in Go, no extra npm parser). "All fields" with field search; descriptions from the schema.
- Checks: Deployment selector vs pod labels; Service selector shows how many pods it matches.
- Every save goes through the Guard review (server-side apply, field manager `kupi`).
- Editor and terminal are code-split: main bundle 278 KB (88 KB gzipped).
- Deviation from the design doc: a custom schema form renderer instead of JSON Forms and react-hook-form, because Kubernetes schemas (int-or-string, quantities, recursion) fit poorly; no extra dependencies.

- "Edit with form or YAML" on a live object verified in the UI; every form input has an accessible name.
- Integration test `internal/bridge/integration_test.go`: clean editable YAML, stable YAML round trip, Secrets refused, selector matching, schemas.

## Milestone 4 status

Done and verified (unit tests, k3d integration test, clicked through in server mode):

- Metrics from metrics-server (`internal/metrics`): a sampler per context that runs only while a metrics view is open and stops after 2 idle minutes; one hour of history at 10 s in memory. Overview page (nodes against allocatable, busiest pods) and a pod Metrics tab (uPlot charts with request and limit lines). A missing metrics-server is reported in plain words.
- Port-forward manager (`internal/forward`): Pods, Services (targetPort names resolved) and workloads; picks a ready pod; listens on 127.0.0.1 only; reconnects to a replacement pod on the same local port. Verified by deleting the pod behind a live forward: traffic kept flowing on the same port. RBAC `pods/portforward` checked first; every forward start is audited.
- Charts load only with the Metrics tab (54 KB chunk).

Not yet:

- Prometheus as a metrics source, and the kubelet summary fallback for clusters without metrics-server.
- System tray list of forwards; saved forwards per project.

## Milestone 5 status

Done and verified (unit tests, clicked through in server mode):

- `internal/graph` builds the map from real relationships: Ingress/HTTPRoute → Service → workload (Service selectors matched against pod-template labels), workload → ConfigMap/Secret/claim (volumes, env, envFrom, projected, image pull secrets), autoscaler → target. Pods fold into their workload (ReplicaSets into Deployments, Jobs into CronJobs) with ready counts, restarts and the worst pod state.
- Missing references (a Secret a pod needs, an Ingress backend) show as dashed red "doesn't exist" nodes. Secret and ConfigMap names come from the table API, so Secret values are never read.
- Map page (React Flow + ELK layered layout, left to right), namespace frames when viewing all namespaces, refresh every 15 s without re-layout unless objects change.
- Overlays: health, exposure (everything reachable from public entry points) and security (privileged, host network, host path, may run as root, no memory limit). Clicking an object focuses its neighbourhood, shows its flags and, for config and storage, which workloads a change affects; the detail panel opens beside the map.
- The map chunk is 1.66 MB (ELK); it loads only when the map opens.

Not yet (later milestones): activity replay, "can A talk to B?" for NetworkPolicies, and the Build (drag-and-drop) canvas.

## Milestone 6 status

Done and verified (unit tests, integration tests against Docker 29 and k3d, clicked through in server mode):

- `internal/engine`: a small Docker Engine API client (Docker and Podman) over the Unix socket or Windows named pipe. TCP endpoints are refused. Chosen over the full Docker SDK to keep the dependency tree small; the only new module is Microsoft's go-winio for named pipes.
- Images page: local images (size, age, in use), details with a layer explorer, environment variable names only (values can hold baked-in secrets), and a root-user warning. Docker Hub search with Official badges and a look-alike warning for typo-squatted names; recent tags from Docker Hub; pull any reference with live progress; remove; disk usage with prune actions.
- "Load into cluster" for k3d and kind is Kupi's own importer, not `k3d image import`. With Docker's containerd image store, k3d's import fails inside the node ("content digest not found") but still reports success. Kupi exports only the node platform (`docker save --platform` piped into `ctr images import` in each node, process to process, no shell), then verifies the image is in every node. minikube uses `minikube image load`. Only clusters labelled local qualify; every load is audited.
- "Deploy" opens the editor with a secure Deployment for the image, with `imagePullPolicy: IfNotPresent`.
- Local clusters page: create (API on 127.0.0.1, never switches the kubectl context, optional agents and k3s version), start, stop, delete (typed confirmation). Create and delete are audited.

Not yet:

- Image vulnerability scanning and signature checks (design: v0.3).
- Private registry logins for pulls (comes with the secrets milestone).
- Tag lists for registries other than Docker Hub.

## Milestone 7 status

Done and verified (unit tests, k3d integration test, clicked through in server mode):

- Secrets page (`internal/secrets`, `SecretService`): key names and sizes only; TLS details (names, expiry, chain, key match, weak keys); registry servers for pull secrets; who uses the Secret (env, envFrom, volumes, projected volumes, image pull secrets, Ingress TLS, service accounts) and which workloads need a restart to see changes, with a Restart button through the Guard.
- Values leave Go only when the user reveals one key. Every reveal and copy is written to the audit log first; production asks for a reason (`guard.RecordAccess`). Revealed values hide again after 30 s. Copy goes from Go straight to the system clipboard (`internal/clipboard`), so the UI never holds the value, and the clipboard is cleared after 30 s if it still holds it (and on exit).
- Editing goes through the Guard as a merge patch with a resourceVersion precondition: only keys the user replaces, adds or removes are sent; current values are never loaded into the form. The review diff shows key names only. Create forms for Opaque, TLS (certificate and key checked as a pair first), registry login (`.dockerconfigjson` built in Go), basic-auth and ssh-auth; `.env` import; masked inputs with spellcheck and autofill off.
- Managed-by detection with a warning before editing: External Secrets, Sealed Secrets, cert-manager, Flux (possibly SOPS), Argo CD, Helm, service-account tokens, Helm release records. Helm release records and token Secrets can't be edited.
- Exposure scan: credentials in ConfigMaps, plain env values and command-line flags (by name and by shape: private keys, cloud and Git tokens, JWTs, passwords in URLs), and Secrets whose values kubectl copied into the last-applied annotation. Findings never include the value. No false positives on a fresh k3s cluster.
- Local vault (`internal/vault`, `VaultService`): items of key/value pairs encrypted with age (X25519). The key lives in Windows Credential Manager / the Secret Service / KWallet (99designs/keyring), or in a passphrase-protected file (age scrypt) when there is none. Writes need only the public key. Auto-locks after 15 minutes. "Create Secret" sends an item to a cluster through the Guard review. Deleting the vault needs typed confirmation. A config dir set through `KUPI_CONFIG_DIR` gets its own password-store entry, so tests never touch the real vault key.
- New dependencies, both named in the design doc: `filippo.io/age`, `github.com/99designs/keyring`.

Not verified:

- Copy to the native clipboard: server mode has no clipboard, so only the unit test covers it. Check in the desktop window.
- The Secret Service and KWallet paths of the vault (only Windows Credential Manager was exercised). Check in WSL2/Linux.

Known limits:

- ~~Windows clipboard history~~: since milestone 9, copies are marked as excluded from clipboard history, cloud sync and clipboard monitors. Linux clipboard managers may still keep history.
- Go can't reliably wipe strings, so decrypted vault values stay in process memory until garbage-collected after locking.
- SOPS, Sealed Secrets and External Secrets are detected and explained, not created or decrypted.
- Kupi's own image pulls (Images page) still don't use registry logins.

## Milestone 8 status

Done and verified (unit tests, k3d integration tests, clicked through in server mode):

- Resource history (`internal/history`): while a cluster is connected, Kupi watches Deployments, StatefulSets, DaemonSets, CronJobs, Services, Ingresses, NetworkPolicies, ConfigMaps, Secrets (metadata only), HPAs and PVCs, and records each real change (status and server fields ignored) with the tool that made it, from `managedFields`, and a diff. Secret value changes are noted without values. Warning events are saved too. Records are append-only JSON lines per cluster per day in the config dir, kept 14 days. History page, plus a History tab on each recorded object. Verified: a `kubectl patch` shows as "changed by kubectl patch" with its diff.
- Incident view (`internal/incident`): failing pods (crash loops, image pulls, unschedulable, OOM kills, not ready), workloads short of copies or with stuck rollouts, failed Jobs, unbound or lost claims, unhealthy nodes, and Services with no ready pods; error lines from the logs of up to 6 failing containers; recorded changes and warnings in the window, merged into one timeline. Verified on the demo crash-looping pod.
- Argo CD and Flux (`internal/gitops`): GitOps page with sync and health, source repository (opens in the browser, https only), revisions, destination and resource counts; Refresh and Sync (Argo CD), Reconcile, Suspend and Resume (Flux), each through the Guard review. Objects owned by an app show a "Managed by" banner with the app's state. Test fixtures: `docs/testdata/gitops-fixtures.yaml` (CRDs and sample apps, no controllers).
- Helm (`helm.sh/helm/v4` SDK, no CLI): release list, history, manifest and resources (Secret values hidden), notes, chart defaults. Values are hidden until asked for, and showing them is audited (with a reason on production). Upgrade with edited values and the same chart: previewed as a server-side dry run (chart schema checked) with a diff of the rendered objects. Roll back to any earlier revision, with a diff. Uninstall, with typed confirmation on production. Every action re-checks the release revision before running, so a stale preview can't run. The SDK lives in `internal/kube`; only the Guard may call `HelmMutator()`, enforced by the architecture tests. Helm's field manager is `helm`, matching the Helm CLI.
- Laptop mode (`internal/laptop`, `localk8s`): per-node memory and CPU use of a k3d cluster against the machine (Docker Engine API), memory and CPU caps on its node containers (kept across restarts), and an optional idle stop. Port-forwards count as use, and connecting starts a cluster that Kupi stopped. Verified: kupi-test measured at 866 MB and 0.11 CPUs; caps applied and lifted.
- Bug found and fixed: a merge patch that set annotations lost them, because the resourceVersion precondition replaced `metadata`.
- Bug found and fixed: Helm v4 names its field manager after the running program, so upgrades from Kupi conflicted with releases installed by other programs.

Not yet:

- Helm: installing new charts from repositories or Artifact Hub, and upgrading to another chart version. Upgrades keep the release's own chart.
- Helm: field-ownership conflicts (for example after a `kubectl edit` of a release's object) fail the upgrade rather than showing in the preview. Kupi never forces them; Helm records the failed revision.
- GitOps: Kupi links to the repository but doesn't open pull requests. Argo CD ApplicationSets aren't listed.
- History covers only the time Kupi is connected, and the listed types.
- Laptop mode is k3d only. With Docker Desktop, "the machine" means Docker's VM. Idle stop counts only Kupi's own use and port-forwards.

Deviations from the design doc:

- History and saved events use append-only JSON lines files instead of SQLite, like the audit log; no SQLite dependency was added.
- The Helm SDK grows the binary: the server-mode test build went from 77 MB to 93 MB.

## Milestone 9 status

Final checks before maintainer testing, done:

- Security sweep against the 11 rules: every bridge method validates its inputs (most in their first lines, the rest one call down); no logging beyond fatal startup errors (the Helm SDK's logs are discarded); every file write goes through the user-only, atomic helpers; k3d and Docker are run with argument lists and validated names; the frontend has no unsafe HTML, no remote code, and opens only https repository links and 127.0.0.1 forward addresses. The architecture tests now also cover Helm.
- Dependencies: `npm audit` clean. `govulncheck` found two reachable advisories in `jose2go` (pulled in by the keyring library's file backend, which Kupi never enables); upgraded to 1.7.0, and `golang.org/x/net` to 0.62.0. One advisory remains in `golang.org/x/crypto`: Kupi doesn't call the affected code and no tagged fix exists yet.
- Builds: every internal package compiles and vets for Linux (`GOOS=linux`); the Windows production binary (63.7 MB, production tags, stripped) starts its native window with WebView2. The Linux app itself still needs building in WSL2 (cgo and WebKitGTK).
- Fixed: connecting to a k3d context whose kubeconfig points at `host.docker.internal` now explains the stale-address problem and the fix when it times out.
- Fixed: on Windows, copied Secret and vault values are marked as excluded from clipboard history (Win+V), cloud clipboard sync and clipboard monitors. Verified against the real clipboard.
- Fixed: history changes with the same timestamp could come out oldest first (a flaky test caught it).
- CI: every action pinned to a commit SHA (checkout v7.0.1, setup-go v7.0.0, setup-node v7.1.0) and a CodeQL workflow for Go and TypeScript added. Neither has run yet: there's no remote.
- Docs: `docs/TESTING.md` is the hands-on checklist for maintainer testing; the README lists what Kupi does and how to run the tests.
- Local builds of everything at once can run out of memory with the Helm SDK; `-p 2` avoids it.

## Milestone 10 status

Requested by the maintainer after the first round of testing on Windows. Done and verified in a browser against k3d (and the terminal against real shells):

- **Multi-object manifests.** `guard.PrepareApplyBatch`: Helm install order, per-object permission check, dry run and diff, one confirmation, one audit entry per object, resourceVersion preconditions. Objects in a namespace or of a CRD type the manifest creates are "checked when applied", and Commit waits for a new CRD to be served. The first failure stops the run. Verified: a Service, Deployment, ConfigMap and their new Namespace applied in one go.
- **Open file:** read in the web view and sent as text (1 MB max). Drag and drop isn't wired: Wails blocks external file drops unless `EnableFileDrop` is on, which would hand file paths to Go.
- **Form suggestions:** comboboxes for fields that name another object (16 types, plus ConfigMap and Secret key names, never values), and a warning for names that don't exist yet. **Sliders** for CPU, memory, storage and replicas, plus a "limit below request" check.
- **Pods on the map:** `graph.PodsOf` follows ownership (Deployment → ReplicaSet → Pod, CronJob → Job → Pod) or a Service's selector. Pod cards fan out under the selected node, the map pans to them, a list sits in the side panel, and a click opens the pod.
- **Settings page** backed by `internal/settings` (clamped ranges, audited safety changes). The clipboard timeout, vault auto-lock and history retention are now configurable; theme (System, Dark, Light) and interface size are applied live.
- **Integrated terminal** (`internal/terminal`, ConPTY and `/dev/ptmx`, no new Go dependency). It's off until confirmed in a native dialog. Only detected shells start, with fixed arguments, enforced by `archtest`. kubectl follows Kupi's cluster and namespace through a credential-free kubeconfig overlay. Tabs, paste check, snippets, search, tools and version skew, and links. CLAUDE.md rules 3 and 6 now describe this exception. Verified: Windows PowerShell in ConPTY, `kubectl get pods` scoped to the connected namespace, `kubectl config set-context` inside the tab leaving `~/.kube/config` untouched, the overlay removed on close, audit entries.
- **Look:** a navy palette from the logo, dark and light themes, the logo in the sidebar and About, a new app icon (`build/appicon.png`, `icon.ico`), slimmer scrollbars, and grouped sidebar sections.
- New dependencies, approved by the maintainer: `@xterm/addon-search`, `@xterm/addon-web-links`.
- Fixed in passing: helper programs (k3d, docker) no longer flash a console window on Windows (`internal/procutil`).
- Not verified yet: the native confirmation dialog in the release build (browser testing used the development-only auto-confirm), the Linux terminal (compiles and vets for Linux; needs a run in WSL2).

## Environment

- Go 1.27, Wails v3.1.0, client-go v0.37 (supports Kubernetes 1.34 to 1.37), k3d 5.9, Node 22.
- Developed on Windows; the Linux build is tested in WSL2 Ubuntu.
- k3d on Windows: create clusters with `--api-port 127.0.0.1:<port>`; the default `host.docker.internal` address can resolve to a stale IP.
- UI testing without a native window: build with `-tags server` and run with `WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=<port> KUPI_CONFIG_DIR=<temp dir>`. Never ship a server-mode build.

## Log

- 2026-11-05: project generated from the Wails v3 React template; macOS, iOS and Android build files removed.
- 2026-11-05: milestone 1 foundation implemented and verified.
- 2026-11-05: milestone 2 core views implemented and verified.
- 2026-11-06: milestone 10 (maintainer feedback, round 1) implemented and verified.
