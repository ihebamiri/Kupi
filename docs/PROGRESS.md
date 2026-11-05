# Progress

Scope of the current build: v0.1 + the v0.2 Observe canvas and v0.2 features, per the design doc.

## Milestones

- [x] 1. Foundation: kubeconfig loading, trust prompt, profiles, sessions, Guard, audit log, security headers, architecture tests. Reviewed and committed.
- [x] 2. Core views: resource browser (all types via discovery), logs, shell, events.
- [x] 3. Forms with YAML sync (hand-designed forms for the main types, generated forms for the rest).
- [x] 4. Live metrics and port-forward manager.
- [x] 5. Observe canvas.
- [x] 6. Images and k3d local clusters.
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

## Environment

- Go 1.27, Wails v3.1.0, client-go v0.37 (supports Kubernetes 1.34 to 1.37), k3d 5.9, Node 22.
- Developed on Windows; the Linux build is tested in WSL2 Ubuntu.
- k3d on Windows: create clusters with `--api-port 127.0.0.1:<port>`; the default `host.docker.internal` address can resolve to a stale IP.
- UI testing without a native window: build with `-tags server` and run with `WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=<port> KUPI_CONFIG_DIR=<temp dir>`. Never ship a server-mode build.

## Log

- 2026-11-05: project generated from the Wails v3 React template; macOS, iOS and Android build files removed.
- 2026-11-05: milestone 1 foundation implemented and verified.
- 2026-11-05: milestone 2 core views implemented and verified.
