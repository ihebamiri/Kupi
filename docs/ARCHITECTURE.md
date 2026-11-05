# How Kupi works

This document explains Kupi from the ground up: what was built, how the pieces fit together, and how each part works. It is written for the maintainer and for anyone who wants to change the code.

Contents:

1. [What was built](#1-what-was-built)
2. [The big picture](#2-the-big-picture)
3. [What happens when Kupi starts](#3-what-happens-when-kupi-starts)
4. [The bridge: how the UI talks to Go](#4-the-bridge-how-the-ui-talks-to-go)
5. [Reaching clusters: kubeconfig, trust, profiles, sessions](#5-reaching-clusters)
6. [The Guard: the only way to change a cluster](#6-the-guard-the-only-way-to-change-a-cluster)
7. [The audit log](#7-the-audit-log)
8. [Feature packages, one by one](#8-feature-packages-one-by-one)
9. [The frontend](#9-the-frontend)
10. [Security model](#10-security-model)
11. [Files Kupi keeps on disk](#11-files-kupi-keeps-on-disk)
12. [Tests and CI](#12-tests-and-ci)
13. [Dependencies and why each is there](#13-dependencies-and-why-each-is-there)
14. [Known limits and deviations from the design](#14-known-limits-and-deviations-from-the-design)
15. [Repository map](#15-repository-map)

---

## 1. What was built

Kupi (Kubernetes Pilot) is a desktop app for managing Kubernetes clusters, from a local test cluster on a laptop to production. The scope of this build is version 0.1 of the design doc plus the version 0.2 "Observe" map and the rest of the 0.2 features, delivered in nine milestones.

| Milestone | What it added |
|---|---|
| 1. Foundation | Kubeconfig loading, the trust prompt for credential plugins, cluster profiles, cluster sessions, the Guard, the audit log, security headers, architecture tests |
| 2. Core views | Resource browser for every type the cluster serves (custom resources included), logs, shell, events, command palette |
| 3. Forms | Forms generated from each cluster's own schema, kept in sync with a YAML editor; curated layouts and secure starter templates |
| 4. Metrics and port-forwards | CPU and memory charts from metrics-server; port-forwards that reconnect on their own |
| 5. Observe map | A live map of how Ingresses, Services, workloads, config and storage connect, with health, exposure and security overlays |
| 6. Images and local clusters | A small Docker/Podman client: images, Docker Hub search, pulls, loading images into k3d/kind; creating and managing k3d clusters |
| 7. Secrets and vault | Secret viewing without leaks, audited reveals, safe editing, TLS and registry Secrets, exposure scan; a local encrypted vault |
| 8. Operations | Change history, saved warnings, incident view, Argo CD and Flux awareness, Helm releases, laptop mode |
| 9. Final checks | Security sweep, dependency audit, build checks, fixes, CI hardening, documentation |

In numbers: 64 Go source files (about 14,700 lines) in 30 internal packages, 72 TypeScript/React files (about 8,600 lines), and 43 Go test files with 99 test functions plus 34 frontend tests. Every milestone was checked three ways: unit tests, integration tests against a real k3d cluster, and clicking through the UI.

The project status, milestone by milestone, with what was verified and what is still missing, is in [PROGRESS.md](PROGRESS.md).

---

## 2. The big picture

Kupi is **one process** on your computer. There is no server, no account and no telemetry.

```
┌───────────────────────────── Kupi process ─────────────────────────────┐
│                                                                        │
│  ┌──────────────────────────┐        ┌───────────────────────────────┐ │
│  │  Web view (UI)           │ calls  │  Go backend                   │ │
│  │  React + TypeScript      │ ─────▶ │                               │ │
│  │  WebView2 on Windows,    │        │  bridge   (the only entry)    │ │
│  │  WebKitGTK on Linux      │ ◀───── │    │                          │ │
│  │                          │ events │    ├─▶ guard (all changes)    │ │
│  └──────────────────────────┘        │    │     │                    │ │
│                                      │    │     ▼                    │ │
│                                      │    ├─▶ kube (cluster access)  │─┼─▶ Kubernetes API servers
│                                      │    ├─▶ engine (Docker/Podman) │─┼─▶ Docker or Podman
│                                      │    ├─▶ localk8s (k3d CLI)     │─┼─▶ k3d
│                                      │    └─▶ audit, vault, history… │─┼─▶ files in your config dir
│                                      └───────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────────┘
```

Four ideas shape everything:

1. **The UI is untrusted.** The web view shows data from clusters, and cluster data can be written by anyone who can create a pod or an annotation. If a page ever ran injected script, that script could call Kupi's backend. So the backend treats every call from the UI like a request from the internet: it validates every input, offers only narrow, specific functions, and never sends a secret value unless the user explicitly asked to reveal it.
2. **One checkpoint for every change.** No feature code can change a cluster directly. Every change is prepared by the Guard (checked, dry-run, diffed), shown to the user, and only then run, and recorded in the audit log.
3. **The cluster decides what's normal.** Kupi doesn't hard-code resource types or forms: it asks each cluster what types it serves (discovery), how they look in a list (the server's table format) and what fields they have (OpenAPI schema). That's why custom resources work everywhere.
4. **Defaults follow the cluster.** A local test cluster is fast and direct; production is careful: read-only until switched on, a written reason for every change, names typed to confirm deletes.

The app is built with **Go** and **Wails v3**. Wails runs the Go backend and the operating system's own web view in one process and generates TypeScript functions for the Go methods the UI may call. Go was chosen because the Kubernetes client, the Helm SDK and the other libraries Kupi needs are native to Go.

---

## 3. What happens when Kupi starts

`main.go` only wires things together. In order:

1. **Config dir.** `internal/appdir` finds the per-user config folder (`%AppData%\kupi` on Windows, `~/.config/kupi` on Linux; `KUPI_CONFIG_DIR` overrides it) and creates it with user-only permissions.
2. **Stores.** It opens the trust store (`trust.json`), the profile overrides (`profiles.json`), the audit log (`audit.log`), the history store (`history/`), the vault (`vault*.age`, `vault.json`) and the laptop-mode settings (`laptop.json`).
3. **Managers.** It creates the cluster session manager (`kube.Manager`), the Guard, the live-stream manager (watches, logs, shells), the metrics sampler, the port-forward manager, the history recorder, the secure clipboard and the laptop-mode manager. Laptop mode starts its once-a-minute check.
4. **Services.** It registers the 19 bridge services with Wails: App, Cluster, Guard, Audit, Resource, Log, Shell, Form, Metrics, Forward, Canvas, Image, LocalCluster, Secret, Vault, History, Incident, GitOps and Helm.
5. **Events.** It registers the typed events the backend pushes to the UI: list changes, log batches, shell output, port-forward changes and image pull progress.
6. **Security headers.** In production builds, every page is served with a strict Content Security Policy: only Kupi's own bundled scripts, no remote scripts, styles or fonts, no frames, no forms posting anywhere.
7. **Window.** It opens one window (1280×800, minimum 960×600). Production builds disable the browser's right-click menu.
8. **Shutdown.** When the window closes, Kupi stops every log stream, shell, watch, port-forward and metrics sampler, clears the clipboard if it still holds a copied secret, locks the vault, stops recording history and stops laptop mode.

---

## 4. The bridge: how the UI talks to Go

`internal/bridge` is the **only** Go code the UI can call. Each Wails service is a Go struct; Wails generates a TypeScript function for each of its exported methods into `frontend/bindings/` (`wails3 generate bindings -ts -i`). The UI imports them through `frontend/src/lib/api.ts`.

Every bridge method follows the same pattern:

1. **Validate** every argument (`internal/validate`): context names, namespaces and object names against Kubernetes' own rules, resource types, plan and stream IDs against strict patterns, sizes against limits (manifests, Secret values, Helm values, passphrases).
2. **Look up** the live session for the context, if the method works on a cluster. A disconnected context is an error, never a silent reconnect.
3. **Call** one package that does the work.
4. **Return** a plain data object built for the UI (a "DTO"). DTOs never include Secret values, credentials or tokens, except the explicit reveal methods.

There are no catch-all methods: no "run this command", "send this HTTP request" or "read this file". Each method does one specific thing.

Long-running data flows the other way through **events**:

| Event | Sent when |
|---|---|
| `kupi:changed` | a watched list changed (at most once every 750 ms per list); the UI reloads it |
| `kupi:logs` | a batch of log lines arrived (flushed every 100 ms) |
| `kupi:shell` | a shell produced output |
| forward changes | a port-forward started, reconnected or stopped |
| pull progress | an image pull made progress |

---

## 5. Reaching clusters

### Kubeconfig loading (`internal/kubeconfig`)

Kupi reads the same kubeconfig files as `kubectl` (`KUBECONFIG`, or `~/.kube/config`). It lists every context with its cluster, user, namespace and API server address. It never switches your current kubectl context. Credentials stay inside the Go process; the UI only sees names, the server address and, for credential plugins, the command and argument list plus the *names* of the environment variables they set.

### Trust for credential plugins (`internal/trust`)

A kubeconfig can contain an `exec` credential plugin: a program that runs when a client connects (for example `aws eks get-token`). That means opening a kubeconfig someone sent you can run their program. So before Kupi connects with such a context it shows the exact command and asks you to trust that file. Trust is pinned to the file's SHA-256 hash: if the file changes in any way, Kupi asks again. Trusted files are recorded in `trust.json`.

### Profiles (`internal/profile`)

Every context gets a profile, which sets how careful Kupi is:

| Profile | Detected when | Editing | Reason needed | Name typed to delete | API rate limit |
|---|---|---|---|---|---|
| local | context or cluster name starts with `kind-`, `k3d-`, `minikube`, `docker-desktop`, `rancher-desktop`; or the server is on 127.0.0.1 / localhost | on | no | no | 50 requests/s |
| dev | name contains a word like dev, test, sandbox | on | no | no | 20 requests/s |
| staging | name contains a word like staging, stage, uat, qa, preprod | on | no | no | 20 requests/s |
| production | name contains prod, prd, production or live, **or Kupi can't tell** | off until switched on with a reason | yes | yes | 10 requests/s |

Unknown clusters are treated as production on purpose: a wrong guess should be the careful one. You can override the profile per context on the Clusters page; overrides are saved in `profiles.json`. Changing a profile disconnects the context, so the new rules apply from the next connect.

### Sessions (`internal/kube`)

Connecting builds a session with several Kubernetes clients, all from the same credentials:

- a **dynamic** client (any resource type, as JSON),
- a **typed** client using **protobuf**, which is faster for built-in types,
- a **metadata-only** client, which watches objects without transferring their contents (so Secret values never travel over a watch),
- **discovery** and a cached **REST mapper** (which kinds exist, and their resource names).

The session exposes two interfaces:

- **`Reader`**, given to everyone: get, list, watch, the server's table format, permission checks (`SelfSubjectAccessReview`), resource types, logs, metadata watches and OpenAPI schemas (cached for 10 minutes).
- **`Mutator`**, reserved for the Guard: server-side apply, merge patch, delete and exec.

Helm has the same split: `Session.Helm()` reads releases, `Session.HelmMutator()` changes them, and only the Guard may call the latter. Port-forwarding also lives here (`kube/forward.go`).

`internal/kube` is the only package allowed to import the low-level client-go packages that talk to the API server (`dynamic`, `kubernetes`, `rest`, `remotecommand`, `portforward`) and the parts of Helm that do (`action`, `kube`, `storage`). The architecture tests fail the build otherwise.

---

## 6. The Guard: the only way to change a cluster

`internal/guard` is the single checkpoint for every change. A change always happens in two steps.

### Step 1: Prepare

A `Prepare…` function builds a **plan** without changing anything:

1. **Profile rules.** If editing is off for this context (production by default), the plan is marked "not allowed" with the reason.
2. **Permissions.** It asks the API server whether you may do it (`SelfSubjectAccessReview`), so a missing permission shows up before you click, in plain words.
3. **Dry run.** It sends the real request with `dryRun=All`. The API server runs everything except storing the result: validation, defaulting and the cluster's admission policies (OPA Gatekeeper, Kyverno and so on). If the cluster would reject the change, you see why now.
4. **Diff.** It renders the object before and after as YAML, without server-managed fields (status, managed fields, resource version), and produces a unified diff. Secret values are never in the diff: each key shows as unchanged, changed or removed (`internal/redact`).
5. **Summary and warnings.** A plain-language summary ("Scales Deployment shop/api from 2 to 5 copies") and warnings: a HorizontalPodAutoscaler that will undo a scale, an owner such as Helm, Argo CD or Flux that will revert the change, a Secret generated by External Secrets or cert-manager.
6. **Store.** The plan gets a random ID and expires after 5 minutes.

The UI shows the plan in the review dialog: summary, warnings, diff, and the reason and confirmation the profile needs.

### Step 2: Commit

`Commit` runs a plan once:

- The plan must exist, be unused and unexpired, and its context must still be connected with the same profile.
- Editing must still be on; the reason must be given if the profile requires one; the name must be typed exactly for confirm-by-name actions.
- The plan is marked used **before** it runs, so it can never run twice.
- Changes carry the **resourceVersion** seen at Prepare time (and Helm actions the release revision), so if anyone changed the object since you reviewed it, the change fails instead of overwriting their work.
- The outcome, success or failure, goes to the audit log with the diff's SHA-256 hash (never the diff itself).

### What the Guard can do

| Prepare function | Used by |
|---|---|
| `PrepareApply` | Create or edit (forms and YAML): server-side apply with field manager `kupi`; conflicts with other managers are shown and only taken over if you choose to |
| `PrepareDelete` | Delete buttons |
| `PrepareScale`, `PrepareRestart` | Workload actions; restart sets the standard `restartedAt` annotation, like `kubectl rollout restart` |
| `PrepareShell` / `StartShell` | Opening a shell in a container (checks `pods/exec`, asks a reason on production, audits before the shell opens) |
| `PrepareSecretWrite` | Secret create and edit: a merge patch with only the keys you changed |
| `PrepareGitOps` | Argo CD refresh and sync; Flux reconcile, suspend and resume |
| `PrepareHelmUpgrade`, `PrepareHelmRollback`, `PrepareHelmUninstall` | Helm releases |

The Guard also records **access** to sensitive data that isn't a change: revealing or copying a Secret value, and showing Helm values (`RecordAccess`). On production these need a reason, and if the audit entry can't be written the value is not handed out.

---

## 7. The audit log

`internal/audit` keeps `audit.log`, one JSON line per entry: time, context, profile, your OS user, your Kubernetes user, action, resource, reason, outcome, error, and the diff hash.

Each entry also stores the hash of the previous entry, and its own hash is the SHA-256 of the entry. Editing or deleting any past line breaks the chain, and "Verify" on the Audit log page reports where. It is **tamper-evident**, not tamper-proof: someone with access to your account can delete the whole file, but can't quietly rewrite history.

Audited actions include every change, failed changes, shell sessions, Secret reveals and copies, Helm values reveals, port-forward starts, editing switched on and off, trust and profile changes, local cluster create, delete, caps and idle stops, and image loads.

---

## 8. Feature packages, one by one

### Resource browser (`catalog`, `live`, bridge `resources.go`)

- **Types.** `catalog` turns the cluster's discovery data into the sidebar: every listable type, grouped into Workloads, Network, Config, Storage, Access control, Cluster and Custom resources.
- **Lists.** Lists use the server's own table format, so the columns are exactly what `kubectl get` shows, including the printer columns custom resources define. Rows carry only metadata.
- **Live updates.** `live` watches each open list with a metadata-only watch and tells the UI at most once every 750 ms to reload. Watches reconnect with backoff; an expired watch resets the list.
- **Details.** YAML views strip `managedFields` and the `last-applied-configuration` annotation (which, for Secrets, holds their values) and hide Secret values.
- **Logs.** One pod, or every pod of a workload merged with a prefix per pod; follow, previous container, timestamps, filter and pause; at most 50,000 lines per view.
- **Shell.** An xterm.js terminal over the exec websocket (with SPDY fallback). Input and output go through events; the shell is opened only through the Guard.

### Forms and YAML (`schema`, bridge `forms.go`, frontend `SchemaForm`, `YamlEditor`)

- `schema` reads the cluster's OpenAPI v3 document for the type's group and version, resolves references, and returns a field tree with types, descriptions, enums, required fields and Kubernetes quirks (int-or-string, quantities).
- The form and the CodeMirror YAML editor edit the same object; switching between them is lossless. YAML is parsed and rendered in Go, so there's one parser.
- Curated layouts show the important fields first for Deployment, Service, ConfigMap, Namespace, Job, CronJob, PersistentVolumeClaim, Ingress and HTTPRoute; "All fields" with search shows the rest. Starter templates are secure by default: non-root, read-only root filesystem, no privilege escalation, all capabilities dropped, seccomp, resource limits.
- Checks: a Deployment's selector must match its pod labels; a Service shows how many pods its selector matches.
- Saving goes through `PrepareApply`.

### Metrics (`metrics`)

When a metrics view is open, a sampler per context reads metrics-server every 10 seconds and keeps one hour in memory. It stops after 2 minutes with no viewer. The Overview page shows nodes against their allocatable capacity and the busiest pods; the pod Metrics tab charts CPU and memory with request and limit lines.

### Port-forwards (`forward`, `kube/forward.go`)

A forward targets a Pod, a Service (named target ports are resolved) or a workload. Kupi picks a ready pod, listens on **127.0.0.1 only**, and when the pod restarts or is replaced it reconnects to a new ready pod on the same local port. Every forward start is permission-checked (`pods/portforward`) and audited.

### The Observe map (`graph`, bridge `canvas.go`, frontend `CanvasPage`)

`graph` builds the map from real relationships:

- Ingress and HTTPRoute → Service (by backend name)
- Service → workload (the Service selector matched against pod template labels)
- workload → ConfigMap, Secret, PersistentVolumeClaim (volumes, env, envFrom, projected volumes, image pull secrets)
- HorizontalPodAutoscaler → its target

Pods fold into their workload (ReplicaSets into Deployments, Jobs into CronJobs) with ready counts, restarts and the worst pod state. References to objects that don't exist show as dashed red nodes. Secret and ConfigMap names come from the table API, so the map never reads Secret values.

The frontend draws it with React Flow and lays it out with ELK, left to right, refreshing every 15 seconds without moving things unless objects change. Overlays: **health**, **exposure** (everything reachable from a public entry point) and **security** (privileged, host network, host path, may run as root, no memory limit). The map code (about 1.7 MB, mostly ELK) loads only when you open the map.

### Images (`engine`, bridge `images.go`)

`engine` is a small Docker Engine API client that talks over the local Unix socket or Windows named pipe and works with Docker and Podman. TCP endpoints are refused. It covers images, inspect, layer history, Docker Hub search, pulls with progress, remove, prune, disk usage, and (for laptop mode) containers, stats and resource caps. The Images page also warns about images that run as root and about look-alike names of official images, and shows environment variable *names* only, since values can hide baked-in secrets.

### Local clusters (`localk8s`)

`localk8s` runs `k3d` (and `minikube` for image loads) with argument lists and validated names, never a shell. New clusters bind their API to 127.0.0.1 and never switch your kubectl context.

**Loading images into k3d or kind** is Kupi's own importer. `k3d image import` can fail silently with Docker's containerd image store ("content digest not found") while reporting success, so Kupi exports only the node's platform (`docker save --platform`), pipes it into `ctr images import` inside each node, and then checks the image is really there.

### Secrets (`secrets`, `clipboard`, bridge `secrets.go`)

- **Viewing.** The Secrets page shows key names and sizes, never values. TLS Secrets show the certificate's names, expiry, chain, whether the key matches, and weak-key warnings. Registry Secrets show their registry names. "Used by" lists workloads, Ingresses and service accounts that reference the Secret, and marks those that read it into environment variables (they only see changes after a restart, with a Restart button).
- **Revealing.** A value leaves Go only when you reveal one key. The access is audited first, and needs a reason on production. The UI hides it again after 30 seconds.
- **Copying.** Copy goes from Go straight to the system clipboard; the UI never holds the value. `clipboard` keeps only a hash of what it copied and clears the clipboard after 30 seconds if it still holds that text (and at exit). On Windows, copies are also marked as excluded from clipboard history (Win+V), cloud clipboard sync and clipboard monitors.
- **Editing.** Only keys you replace, add or remove are sent, as a merge patch with a resourceVersion precondition. Current values are never loaded into the form. There are forms for Opaque, TLS (the certificate and key are checked as a pair first), registry login (`.dockerconfigjson` built in Go), basic-auth and SSH Secrets, and `.env` import. Inputs are masked and spell-check and autofill are off.
- **Managed Secrets.** Kupi warns when a Secret is written by External Secrets, Sealed Secrets, cert-manager, Flux, Argo CD or Helm, and refuses to edit service-account tokens and Helm release records.
- **Exposure scan.** It finds credentials stored in the open: in ConfigMaps, in plain environment variable values, in command-line flags, and Secret values that `kubectl apply` copied into the `last-applied-configuration` annotation. It recognises them by name (password, token, api_key…) and by shape (private keys, cloud and Git tokens, JWTs, passwords in URLs). Findings never contain the value.

### The vault (`vault`)

The vault keeps named items, each a set of keys and values, encrypted on your computer:

- The data file `vault.age` is encrypted with [age](https://age-encryption.org) to an X25519 key.
- That key lives in the operating system's password store (Windows Credential Manager, or the Secret Service or KWallet on Linux, through `99designs/keyring`), or, if there is none, in `vault-key.age`, encrypted with your passphrase (age's scrypt mode).
- Writes only need the public half of the key. The private key is used only while unlocking.
- Decrypted values are held in memory only while the vault is unlocked, and it locks itself after 15 minutes without use.
- "Create Secret" turns an item into a Kubernetes Secret through the normal Guard review.

### History and saved warnings (`history`)

While a cluster is connected, a recorder watches Deployments, StatefulSets, DaemonSets, CronJobs, Services, Ingresses, NetworkPolicies, ConfigMaps, Secrets (metadata only), HorizontalPodAutoscalers and PersistentVolumeClaims across every namespace you may watch (otherwise your default namespace).

For each object it keeps the last version it saw, without status and server fields. When an object really changes, it records the time, the action (created, changed, deleted), the **field manager** that made the newest write (from `managedFields`: `kubectl-edit`, `helm`, `argocd-controller`, `kupi`…) and a diff. Secret value changes are recorded as a note, never with values. It also saves warning events, since Kubernetes deletes events after about an hour.

Records go to `history/<cluster>/<day>.jsonl`, append-only, kept 14 days. History covers only the time Kupi is connected.

### Incident view (`incident`)

One request builds the report:

- **Failing now:** pods that failed, crash-loop, can't pull their image, can't be scheduled, were OOM-killed recently or stay not-ready; workloads with fewer ready copies than wanted or a stuck rollout; failed Jobs; claims not bound or lost; nodes not ready or under pressure; Services with no ready pods behind them.
- **From the logs:** the error lines (or the last lines) from up to 6 failing containers, from the previous run when the container crashed.
- **What changed:** recorded changes and warning events in the chosen window, on one timeline.

### Argo CD and Flux (`gitops`)

`gitops` reads Argo CD Applications and Flux Kustomizations and HelmReleases (and Flux sources, to find repository addresses): sync and health status, source repository, target and applied revision, destination, resource counts and messages. It recognises which app owns an object from the labels and annotations these tools set, and the detail panel shows a "Managed by" banner.

Actions ask the tool to do its own work and go through the Guard: refresh (compare with Git now) and sync for Argo CD; reconcile, suspend and resume for Flux. Repository links open only `https://` addresses; `git@host:org/repo` is converted to its web address.

### Helm (`kube/helm.go`, `helmrel`, guard `helm.go`)

Kupi uses the Helm v4 Go SDK; it never runs the `helm` program.

- **Reading.** Release list, history, the rendered manifest with Secret values hidden, the objects a release owns, notes, and the chart's default values. A release's own values often contain passwords, so they stay hidden until you ask; showing them is audited, with a reason on production.
- **Upgrade.** Upgrades keep the release's own chart and change its values. The preview is a server-side dry run, so the cluster's admission checks and the chart's values schema both apply, and the review shows the diff of every rendered object.
- **Rollback.** Roll back to any earlier revision, previewed as a diff.
- **Uninstall.** Typed confirmation on production.
- Every action checks again, just before running, that the release is still at the revision you reviewed.
- Helm's server-side apply field manager is `helm`, the same as the Helm CLI, so Kupi and `helm` share ownership instead of conflicting.

### Laptop mode (`laptop`, `localk8s/laptop.go`)

- **Usage:** per-node memory and CPU of a k3d cluster's containers against the machine's totals (Docker Engine stats).
- **Caps:** memory and CPU limits on the node containers. They survive the cluster being stopped and started.
- **Idle stop:** optional. Every bridge call for a context counts as use, and so do open port-forwards. After the chosen time without use, Kupi stops the cluster and records it in the audit log; connecting to it later starts it again.

---

## 9. The frontend

`frontend/` is a React 18 + TypeScript app built with Vite 8, styled with Tailwind CSS v4. It is served from inside the binary (Go `embed`), so nothing loads from the network.

- **Entry and layout:** `App.tsx` draws the sidebar, the context bar and the page. A small router in `state/nav.tsx` holds the current page as data (no URLs).
- **State:** `state/clusters.tsx` (contexts, the active cluster, the namespace in view, connect and disconnect), `state/nav.tsx` (current page, resource types of the active cluster), `state/forwards.tsx` (live port-forward list from events).
- **Pages:** Clusters, Overview, Map, Incident, History, Helm, GitOps, Secrets, Vault, Images, Local clusters, Port forwards, Create or edit, Audit log, and one Resources page per type.
- **Shared components:** the review dialog (every change), the detail panel, the resource table (virtualised, so huge lists stay fast), the logs and shell views, the schema form, the YAML editor, the diff view, the timeline, dialogs and small UI parts.
- **Code splitting:** the YAML editor (CodeMirror), the terminal (xterm.js), the map (React Flow + ELK), the metrics charts (uPlot) and the Helm page load only when first opened. The main bundle is about 200 KB (56 KB compressed).
- **Rules:** no `dangerouslySetInnerHTML`, no `eval`, no remote scripts, styles or fonts; cluster data is always shown as text. Values typed into secret fields are masked, with spell-check and autofill off.

---

## 10. Security model

### What Kupi defends against

1. **Hostile data in the UI.** A pod log, label, annotation, image label or Helm note can contain text designed to look like code. Kupi shows all cluster data as text, never as HTML, ships a strict Content Security Policy, and gives the UI only narrow, validated functions, so even injected script couldn't do much.
2. **A malicious kubeconfig.** Credential plugins run only after you trust the exact file, and trust breaks when the file changes.
3. **Mistakes on production.** Read-only by default, dry run and diff before every change, a reason for every change, typed names for deletes, stale previews refused, and an audit trail.
4. **Secret leaks.** Values aren't fetched for lists, maps, watches or diffs; reveals are explicit, audited and short-lived; copies bypass the UI and clear themselves; the vault is encrypted at rest.

### The eleven rules

The repository's `CLAUDE.md` lists eleven non-negotiable rules. The architecture tests (`internal/archtest`) enforce the mechanical ones on every `go test ./...`:

- only `internal/guard` may call `Mutator()` or `HelmMutator()`;
- only `internal/kube` may import the client-go and Helm packages that talk to the API server;
- nothing may run a shell (`sh`, `bash`, `cmd`, `powershell`…);
- the frontend may not use `dangerouslySetInnerHTML`, `eval` or `new Function`, or load remote resources.

The milestone 9 sweep checked the others by reading the code: every bridge method validates its inputs, nothing logs or returns secret values, every file write is user-only and atomic, port-forwards bind to 127.0.0.1, and unknown clusters are production.

---

## 11. Files Kupi keeps on disk

All in the config dir (`%AppData%\kupi` on Windows, `~/.config/kupi` on Linux), with user-only permissions, written atomically (temporary file, then rename) except append-only logs.

| File | Contents |
|---|---|
| `trust.json` | Kubeconfig files you trusted, with their SHA-256 and plugin commands |
| `profiles.json` | Profile overrides per context |
| `audit.log` | The hash-chained audit log |
| `history/<cluster>/<day>.jsonl` | Recorded changes and warning events, 14 days |
| `vault.json`, `vault.age`, `vault-key.age` | Vault settings, encrypted data, and (passphrase mode only) the encrypted key |
| `laptop.json` | Laptop-mode idle timers |

The vault key itself is in the OS password store under the name `kupi`, unless you chose a passphrase.

---

## 12. Tests and CI

- **Unit tests** (`go test ./...`): each package with fakes for the cluster, Docker and the password store. They include the architecture tests.
- **Integration tests** (`go test -tags integration ./internal/...`, against the k3d cluster `kupi-test`): real create, update, delete and Secret flows through the Guard, history recording, Helm install, upgrade, rollback and uninstall, image loading, laptop-mode caps, the Windows password store and clipboard.
- **Frontend tests** (`cd frontend && npm test`, Vitest): YAML and diff helpers, schema paths, units, templates, `.env` parsing, key/value editing and more.
- **CI** (`.github/workflows/ci.yml`): tests and builds on Ubuntu and Windows, gofmt, go vet, integration tests in a k3d cluster, and govulncheck. `codeql.yml` runs CodeQL on Go and TypeScript. Every action is pinned to a commit SHA. Neither workflow has run yet, since the repository has no remote.

---

## 13. Dependencies and why each is there

| Dependency | Why |
|---|---|
| Wails v3 | Desktop shell: Go backend plus the system web view, typed bindings and events |
| client-go, apimachinery, api (v0.37) | The official Kubernetes client; supports Kubernetes 1.34 to 1.37 |
| Helm v4 SDK | Helm releases without the Helm CLI |
| filippo.io/age | Vault encryption |
| 99designs/keyring | The OS password store for the vault key |
| Microsoft go-winio | Docker's named pipe on Windows |
| golang.org/x/sys | Windows clipboard calls |
| React, TypeScript, Vite, Tailwind | The UI |
| Radix Dialog, lucide-react, cmdk, TanStack Virtual | Accessible dialogs, icons, the command palette, fast long lists |
| CodeMirror 6, xterm.js, uPlot, React Flow, elkjs | YAML editor, terminal, charts, map, map layout |
| Vitest | Frontend tests |

The design doc's technology table names all of these. Kupi avoids the full Docker SDK (a small client of its own instead) and has no SQLite.

---

## 14. Known limits and deviations from the design

**Not built yet:**

- Installing new Helm charts from repositories or Artifact Hub, and upgrading to another chart version.
- Pull requests to Git for GitOps-managed objects. Argo CD ApplicationSets aren't listed.
- Prometheus as a metrics source.
- The "Build" drag-and-drop canvas, activity replay, and the "can A talk to B" NetworkPolicy check.
- Image vulnerability scanning and signature checks.
- Private registry logins for Kupi's own image pulls.
- A system tray.

**Limits of what was built:**

- History covers only the time Kupi is connected.
- Laptop mode works with k3d only.
- On Linux, clipboard managers may keep copied values.
- A Helm upgrade that conflicts with a field someone edited by hand fails rather than showing the conflict in the preview.

**Not verified yet:**

- Daily use in the native window (it starts; the features were clicked through in a browser).
- The Linux build, and the vault on Linux.
- The CI workflows.

**Deviations from the design doc:**

- Forms are rendered by Kupi's own schema form, not JSON Forms with react-hook-form, because Kubernetes schemas fit those poorly.
- History and the audit log use append-only JSON lines files instead of SQLite.
- The vault's passphrase fallback uses age's scrypt mode rather than Argon2id.

---

## 15. Repository map

```
main.go                     app wiring: stores, managers, services, window, CSP
internal/
  appdir/                   config dir, atomic user-only writes
  archtest/                 architecture and security rule tests
  audit/                    hash-chained audit log
  bridge/                   Wails services: the only API the UI can call
  buildmode/                production vs development flag
  catalog/                  resource types → sidebar groups
  clipboard/                self-clearing clipboard; Windows history exclusion
  diff/                     unified diffs
  engine/                   Docker/Podman Engine API client
  forward/                  port-forward manager
  gitops/                   Argo CD and Flux
  graph/                    the Observe map's model
  guard/                    the single checkpoint for changes
  helmrel/                  safe views of Helm releases
  history/                  change and warning recorder
  incident/                 incident report
  kube/                     sessions, Reader/Mutator, Helm client, port-forward
  kubeconfig/               kubeconfig loading
  laptop/                   idle stop and wake
  live/                     watches, log streams, shells
  localk8s/                 k3d/kind/minikube, image loading, usage and caps
  metrics/                  metrics-server sampler
  profile/                  profile detection and rules
  redact/                   hiding Secret values
  schema/                   OpenAPI schemas → form fields
  secrets/                  Secret details, TLS, registry logins, exposure scan
  trust/                    kubeconfig trust store
  validate/                 input validation
  vault/                    encrypted local vault
  version/                  version string, user agent
frontend/
  src/pages/                one file per page
  src/components/           shared UI parts
  src/state/                clusters, navigation, forwards
  src/lib/                  API re-exports and pure helpers (with tests)
  bindings/                 generated by Wails, never edited by hand
build/                      Wails build tasks, icons, Windows manifest and version info
docs/                       this document, the user guide, testing guides, progress, test data
.github/workflows/          CI and CodeQL
```
