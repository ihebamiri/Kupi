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

Kupi (Kubernetes Pilot) is a desktop app for managing Kubernetes clusters, from a local test cluster on a laptop to production. The scope of this build is version 0.1 of the design doc, the version 0.2 "Observe" map and the rest of the 0.2 features, and most of 0.3, delivered in fifteen milestones.

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
| 10. Maintainer feedback | Multi-object manifests and file upload, form suggestions and sliders, pods on the map, Settings, the integrated terminal, the logo and a new look |
| 11. Releases | A tag builds a Windows zip and Linux packages (AppImage, deb, rpm, tar.gz) into a draft GitHub release with checksums |
| 12. Helm charts | Installing charts from Artifact Hub, chart repositories and OCI registries; upgrading a release to another chart version |
| 13. Image security | Private registry logins for pulls, vulnerability scans with Trivy, signature checks with cosign |
| 14. GitOps pull requests | Changes to objects that Argo CD or Flux manage are proposed as GitHub pull requests |
| 15. Observe, round 2 | Prometheus as a long-range metrics source, "can A talk to B?" from NetworkPolicies, replay of recorded changes on the map |

Every milestone was checked with unit tests and, where it touches a cluster, integration tests against a real k3d cluster and by clicking through the UI. The parts that need outside services (Artifact Hub, GitHub, Prometheus, Trivy, cosign) are covered by unit tests with fakes; [PROGRESS.md](PROGRESS.md) says which were also checked live.

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
| `PrepareApplyBatch` | A manifest with several objects: see below |
| `PrepareDelete` | Delete buttons |
| `PrepareScale`, `PrepareRestart` | Workload actions; restart sets the standard `restartedAt` annotation, like `kubectl rollout restart` |
| `PrepareShell` / `StartShell` | Opening a shell in a container (checks `pods/exec`, asks a reason on production, audits before the shell opens) |
| `PrepareSecretWrite` | Secret create and edit: a merge patch with only the keys you changed |
| `PrepareGitOps` | Argo CD refresh and sync; Flux reconcile, suspend and resume |
| `PrepareHelmUpgrade`, `PrepareHelmRollback`, `PrepareHelmUninstall` | Helm releases |

### Several objects in one manifest

`PrepareApplyBatch` takes every object of a manifest (documents separated by `---`, or a `List`), at most 200:

1. It sorts them in Helm's install order: Namespaces, quotas, ServiceAccounts, Secrets, ConfigMaps, storage, CRDs, RBAC, Services, then workloads, then Ingresses and webhooks; custom resources last.
2. It notes what the manifest itself creates that the cluster lacks: new namespaces, and custom types defined by a CRD in the same manifest.
3. Each object gets the single-object treatment: name and namespace checks, a permission check, a server dry run and a diff. The result is a `PlanItem` with a state: create, update, unchanged, blocked, or **deferred**. Objects in a new namespace or of a new type can't be dry-run, so they're shown in full and checked by the cluster at commit. A duplicate object, an unknown type, a denied permission or a rejected dry run blocks the whole plan.
4. Commit applies the steps in order, each with its resourceVersion precondition, and writes **one audit entry per object**. Before an object of a new type, it waits (up to a minute) for its CRD to be Established and the type to be served. Kubernetes has no transactions, so the first failure stops the run and the error says how many objects were applied.

The Guard also records **access** to sensitive data that isn't a change: revealing or copying a Secret value, and showing Helm values (`RecordAccess`). On production these need a reason, and if the audit entry can't be written the value is not handed out.

---

## 7. The audit log

`internal/audit` keeps `audit.log`, one JSON line per entry: time, context, profile, your OS user, your Kubernetes user, action, resource, reason, outcome, error, and the diff hash.

Each entry also stores the hash of the previous entry, and its own hash is the SHA-256 of the entry. Editing or deleting any past line breaks the chain, and "Verify" on the Audit log page reports where. It is **tamper-evident**, not tamper-proof: someone with access to your account can delete the whole file, but can't quietly rewrite history.

Audited actions include every change (one entry per object for a multi-object manifest), failed changes, shell sessions, opening the integrated terminal and switching it on or off, changes to safety settings, Secret reveals and copies, Helm values reveals, port-forward starts, editing switched on and off, trust and profile changes, local cluster create, delete, caps and idle stops, and image loads.

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
- **Suggestions.** `frontend/src/lib/refs.ts` knows which field paths name another object: `metadata.namespace`, `configMapKeyRef.name`, `secretRef.name`, `persistentVolumeClaim.claimName`, `serviceAccountName`, `storageClassName`, `ingressClassName`, Ingress backends, `scaleTargetRef`, `roleRef`, RBAC subjects and more. Those fields become a combobox (`FormInputs.tsx`). It asks `FormService.Suggest` for the names of one of 16 allowed types, or `FormService.Keys` for the key names of a ConfigMap or Secret. Only key names: values never leave Go. Results are cached for 20 seconds.
- **Sliders.** CPU, memory and storage in `resources.requests` and `limits`, and replica counts, get a slider over sensible steps (10m to 16 CPUs, 16Mi to 64Gi…) next to the exact value. `lib/quantity.ts` parses Kubernetes quantities and flags a limit below its request.
- **Several objects.** `FormService.FromYAML` reports the objects of a multi-object manifest instead of one object, and the form steps aside for the YAML view. **Open file** reads the chosen file in the web view (`File.text()`, 1 MB at most) and sends the text like a paste, so Go never opens a path for the UI.
- Saving goes through `PrepareApply`, or `PrepareApplyBatch` for several objects.

### Metrics (`metrics`)

When a metrics view is open, a sampler per context reads metrics-server every 10 seconds and keeps one hour in memory. It stops after 2 minutes with no viewer. The Overview page shows nodes against their allocatable capacity and the busiest pods; the pod Metrics tab charts CPU and memory with request and limit lines.

**Prometheus (`prom`).** For longer ranges (6 hours, 24 hours, 7 days), the Metrics tab asks a Prometheus running in the cluster. `prom.Find` looks for its Service by the names and labels the Prometheus Operator, kube-prometheus-stack and the prometheus chart use. Queries go through the API server's service proxy (`Reader.ServiceProxyGet`, answers capped at 8 MB), so they use the user's own credentials and RBAC, and no port-forward is needed. Kupi runs only two fixed queries (CPU rate and memory working set of one pod, about 120 points per range); the namespace and pod name are validated as DNS names before they go into the query, so nothing from the UI becomes PromQL.

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

Selecting a workload or Service calls `CanvasService.Pods`. It follows ownership (Deployment → ReplicaSets → Pods, CronJob → Jobs → Pods; a Service uses its selector) and returns pod summaries, worst first. The map adds up to 12 pod cards under the selected node and pans to them. The side panel lists them all, and a pod opens its own detail panel.

**Can A talk to B? (`netpol`, bridge `network.go`, frontend `MapTools`).** `NetworkService.CanReach` takes two endpoints (a pod, a workload, or a Service as the destination), picks a running pod for each (a Service maps its port to the pod's target port, named ports included), and reads the NetworkPolicies of both namespaces and the namespace labels. `netpol.Evaluate` applies Kubernetes' rules: a pod is isolated for a direction only when some policy selects it with that policy type; traffic is allowed when any selecting policy has a rule whose peers (pod selector, namespace selector, both together, or an IP block with exceptions) and ports (numbers, names, `endPort` ranges) match. It answers for egress at the source and ingress at the destination separately, with the policies that decided each. It reads policies only; it sends no traffic, and the answer assumes the network plugin enforces policies.

**Replay (frontend `MapTools`).** The replay bar reads the changes `history` recorded (`HistoryService.Changes`, up to 500, oldest first) and steps through them, highlighting each changed object on the map and saying which field manager made the change.

### Images (`engine`, bridge `images.go`)

`engine` is a small Docker Engine API client that talks over the local Unix socket or Windows named pipe and works with Docker and Podman. TCP endpoints are refused. It covers images, inspect, layer history, Docker Hub search, pulls with progress, remove, prune, disk usage, and (for laptop mode) containers, stats and resource caps. The Images page also warns about images that run as root and about look-alike names of official images, and shows environment variable *names* only, since values can hide baked-in secrets.

- **Registry logins** (bridge `registries.go`) are vault items named `registry-<host>` with `username` and `password` keys, so they're encrypted like everything else in the vault. A pull looks up the login for the image's registry (Docker Hub's aliases included) and sends it in the `X-Registry-Auth` header; passwords never go back to the UI.
- **Scans and signatures** (`imagescan`) run [Trivy](https://trivy.dev) (`trivy image --format json --scanners vuln`) and [cosign](https://www.sigstore.dev) (`cosign verify`, keyless) as optional programs through `procutil`, with an absolute path and an argument list, after the image reference is validated. The signature check accepts any Sigstore identity and shows who signed, so the user decides whether that's the publisher they expect. When a tool isn't installed, the UI says where to get it.

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

**Pull requests (`gitpr`, bridge `gitops_pr.go`).** When the user edits an object an app manages, `GitOpsService.ProposeChange` finds the app's GitHub repository, branch and path, lists the YAML files under that path (GitHub's trees API, at most 400 files of 1 MB each), and finds the document whose kind, namespace and name match. It replaces only that document, keeping the file's other documents and comments, and returns the diff. `OpenPullRequest` creates a `kupi/<kind>-<name>-<time>` branch, commits the file and opens the pull request, through GitHub's REST API with the token from the vault item `github-token`. The cluster isn't touched, so this isn't a Guard action; it's audited. Secrets are refused, and only GitHub is supported.

### Helm (`kube/helm.go`, `helmrel`, guard `helm.go`)

Kupi uses the Helm v4 Go SDK; it never runs the `helm` program.

- **Reading.** Release list, history, the rendered manifest with Secret values hidden, the objects a release owns, notes, and the chart's default values. A release's own values often contain passwords, so they stay hidden until you ask; showing them is audited, with a reason on production.
- **Upgrade.** Change the values with the release's own chart, or move to another chart version (`PrepareHelmUpgradeChart`). The preview is a server-side dry run, so the cluster's admission checks and the chart's values schema both apply, and the review shows the diff of every rendered object.
- **Install** (`PrepareHelmInstall`). Charts come from `charts`: Artifact Hub search (only the search text is sent), classic repositories (`index.yaml` over https only, each download checked against the index's digest, size caps of 64 MB for indexes and 32 MB for charts) and OCI registries (Helm's registry client). The release name is validated, permissions are checked for every kind the chart renders, the install is dry-run on the server, and the review warns about cluster-wide objects and hooks (which a dry run can't preview).
- **Rollback.** Roll back to any earlier revision, previewed as a diff.
- **Uninstall.** Typed confirmation on production.
- Every action checks again, just before running, that the release is still at the revision you reviewed.
- Helm's server-side apply field manager is `helm`, the same as the Helm CLI, so Kupi and `helm` share ownership instead of conflicting.

### Laptop mode (`laptop`, `localk8s/laptop.go`)

- **Usage:** per-node memory and CPU of a k3d cluster's containers against the machine's totals (Docker Engine stats).
- **Caps:** memory and CPU limits on the node containers. They survive the cluster being stopped and started.
- **Idle stop:** optional. Every bridge call for a context counts as use, and so do open port-forwards. After the chosen time without use, Kupi stops the cluster and records it in the audit log; connecting to it later starts it again.

### Settings (`settings`, bridge `settings.go`)

`settings.json` holds the preferences: theme, interface size, log and map defaults, the clipboard and vault timeouts, history retention, and the terminal options and snippets. `Normalize` clamps every value to a safe range on load and on save (copied secrets clear within 5 minutes, the vault locks within 8 hours), so a hand-edited file can't switch a safety feature off. The bridge applies changes at once (vault auto-lock, history retention, clipboard timeout) and audits changes to safety settings. `Save` can switch the terminal off but never on.

### The integrated terminal (`terminal`, bridge `terminal.go`)

The one deliberate exception to "never run a shell":

- **Off by default.** `SettingsService.EnableTerminal` shows a **native** OS dialog (Wails' message dialog, outside the web view, so page scripts can't answer it), with Cancel as the default. Development builds can skip it with `KUPI_DEV_AUTOCONFIRM=1` for browser testing; release builds can't.
- **Only detected shells.** `terminal.Shells()` finds PowerShell 7, Windows PowerShell, Command Prompt, Git Bash and WSL on Windows (system paths from `GetSystemDirectory`), and the shells listed in `/etc/shells` (bash, zsh, fish, sh) on Linux. The UI picks one by ID and can't name a path. Arguments are fixed (`-NoLogo`, `--login -i`). The architecture tests reject command flags (`-c`, `/c`, `-Command`…) in this package, and any import of it outside the bridge.
- **Pseudo-terminals without new dependencies.** Windows uses ConPTY (`CreatePseudoConsole` from `golang.org/x/sys/windows`). A watcher closes the console when the shell exits, because ConPTY doesn't end its output stream by itself. Linux opens `/dev/ptmx` with `golang.org/x/sys/unix` and starts the shell in its own session. Output streams as `kupi:terminal` events, split only on whole UTF-8 characters. Input is limited to 64 KB per call, and at most 12 terminals can be open.
- **kubectl follows Kupi.** With a connected cluster, the bridge writes a small kubeconfig to `<config>/terminal/<random>.kubeconfig`. It holds only `current-context` and one context entry: the cluster and user **names**, and the namespace. `KUBECONFIG` is set to that file followed by the real ones. Kubeconfig merging takes the first definition of a context, so the tab uses Kupi's cluster and namespace while credentials still come from the real files, and `kubectl config …` inside the tab writes to the small file. It's removed when the tab closes and on the next start. `KUPI_CONTEXT`, `KUPI_NAMESPACE` and `KUPI_PROFILE` are set; Kupi's own `KUPI_*` variables are not passed on.
- **In the UI** (`TerminalPage`, `TerminalTab`, `lib/snippets.ts`):
  - xterm.js with the fit, search and web-links add-ons;
  - tabs that stay mounted while you visit other pages;
  - a context strip, red on production;
  - a paste check that intercepts every paste and asks before text with a line break, or a destructive command on production;
  - snippets that are typed but never run;
  - a tools panel (`TerminalService.Tools`) with the versions of kubectl, helm, docker, k3d, kind, minikube, flux and argocd, and a kubectl/cluster version-skew warning;
  - links that open in the browser only after a confirmation (`AppService.OpenURL`, http and https only).
- Commands typed in the terminal don't go through the Guard. The UI says so, and opening a terminal is audited.

### Helper programs (`procutil`)

kubectl, docker and k3d are started with `procutil.Command`: an absolute path and an argument list, plus `CREATE_NO_WINDOW` on Windows so no console window flashes over Kupi.

---

## 9. The frontend

`frontend/` is a React 18 + TypeScript app built with Vite 8, styled with Tailwind CSS v4. It is served from inside the binary (Go `embed`), so nothing loads from the network.

- **Entry and layout:** `App.tsx` draws the sidebar, the context bar and the page. A small router in `state/nav.tsx` holds the current page as data (no URLs).
- **State:** `state/settings.tsx` (preferences; applies the theme through `data-theme` on `<html>` and the interface size through the web view's zoom), `state/clusters.tsx` (contexts, the active cluster, the namespace in view, connect and disconnect), `state/nav.tsx` (current page, resource types of the active cluster), `state/forwards.tsx` (live port-forward list from events).
- **Pages:** Clusters, Overview, Map, Incident, History, Helm, GitOps, Secrets, Vault, Images, Local clusters, Port forwards, Create or edit, Terminal, Settings, Audit log, and one Resources page per type.
- **Look:** design tokens in `styles.css` (a navy palette from the logo, dark and light), the logo as an inline SVG component (`components/Logo.tsx`), and the app icon (`build/appicon.png`, `build/windows/icon.ico`) rendered from the same shapes.
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
- nothing may run a shell (`sh`, `bash`, `cmd`, `powershell`…). The integrated terminal is the one exception: no command flag (`-c`, `/c`, `-Command`…) may appear in its package, and only the bridge may import it;
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
| `settings.json` | Preferences and terminal snippets |
| `terminal/*.kubeconfig` | While a terminal tab is open: its context-and-namespace overlay (no credentials) |

The vault key itself is in the OS password store under the name `kupi`, unless you chose a passphrase.

---

## 12. Tests and CI

- **Unit tests** (`go test ./...`): each package with fakes for the cluster, Docker and the password store. They include the architecture tests.
- **Integration tests** (`go test -tags integration ./internal/...`, against the k3d cluster `kupi-test`): real create, update, delete and Secret flows through the Guard, history recording, Helm install, upgrade, rollback and uninstall, image loading, laptop-mode caps, the Windows password store and clipboard.
- **Frontend tests** (`cd frontend && npm test`, Vitest): YAML and diff helpers, schema paths, units, templates, `.env` parsing, key/value editing and more.
- **CI** (`.github/workflows/ci.yml`): tests and builds on Ubuntu 24.04 (GTK 4 and WebKitGTK 6.0) and Windows, gofmt, go vet, integration tests in a k3d cluster, and govulncheck. `codeql.yml` runs CodeQL on Go and TypeScript. Every action is pinned to a commit SHA.
- **Releases** (`.github/workflows/release.yml`): pushing a tag like `v0.2.0` stamps the version (`scripts/set-version.sh`), creates a **draft** GitHub release, builds the Windows zip and the Linux tar.gz, AppImage, deb and rpm, uploads them, and adds `SHA256SUMS`. The maintainer reviews the draft and publishes it. Builds aren't code-signed, so Windows SmartScreen warns on first start.

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
| golang.org/x/sys | Windows clipboard calls, ConPTY and the Linux pseudo-terminal |
| React, TypeScript, Vite, Tailwind | The UI |
| Radix Dialog, lucide-react, cmdk, TanStack Virtual | Accessible dialogs, icons, the command palette, fast long lists |
| CodeMirror 6, xterm.js (with its fit, search and web-links add-ons), uPlot, React Flow, elkjs | YAML editor, terminals, charts, map, map layout |
| Vitest | Frontend tests |

The design doc's technology table names all of these. Kupi avoids the full Docker SDK (a small client of its own instead) and has no SQLite.

---

## 14. Known limits and deviations from the design

**Not built yet:**

- Argo CD ApplicationSets aren't listed. Pull requests work with GitHub only.
- The "Build" drag-and-drop canvas.
- A system tray.
- Code signing (the maintainer chose not to buy a certificate).

**Limits of what was built:**

- History, and so replay, covers only the time Kupi is connected.
- Laptop mode works with k3d only.
- On Linux, clipboard managers may keep copied values.
- A Helm upgrade that conflicts with a field someone edited by hand fails rather than showing the conflict in the preview.
- "Can A talk to B" reads NetworkPolicies only: it doesn't know Cilium or Calico's own policy types, and it can't tell whether the network plugin enforces policies.
- GitOps pull requests need the object written out as plain YAML under the app's path; Kustomize patches and Helm values aren't edited.

**Not verified yet:**

- Daily use in the native window (it starts on Windows and Linux; the features were clicked through in a browser).
- Live runs against Artifact Hub, GitHub, Prometheus, Trivy and cosign (unit tests with fakes cover them).
- The release workflow (it runs on the first tag).

**Deviations from the design doc:**

- Forms are rendered by Kupi's own schema form, not JSON Forms with react-hook-form, because Kubernetes schemas fit those poorly.
- History and the audit log use append-only JSON lines files instead of SQLite.
- The vault's passphrase fallback uses age's scrypt mode rather than Argon2id.
- The integrated terminal, added after v0.2 at the maintainer's request, is a deliberate exception to the "never run a shell" and "no catch-all bindings" rules, bounded as described in section 8.

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
  charts/                   Helm charts: Artifact Hub, repositories, OCI
  clipboard/                self-clearing clipboard; Windows history exclusion
  diff/                     unified diffs
  engine/                   Docker/Podman Engine API client
  forward/                  port-forward manager
  gitops/                   Argo CD and Flux
  gitpr/                    GitHub pull requests for GitOps-managed objects
  graph/                    the Observe map's model
  guard/                    the single checkpoint for changes
  helmrel/                  safe views of Helm releases
  history/                  change and warning recorder
  imagescan/                Trivy scans and cosign signature checks
  incident/                 incident report
  kube/                     sessions, Reader/Mutator, Helm client, port-forward
  kubeconfig/               kubeconfig loading
  laptop/                   idle stop and wake
  live/                     watches, log streams, shells
  localk8s/                 k3d/kind/minikube, image loading, usage and caps
  metrics/                  metrics-server sampler
  netpol/                   NetworkPolicy evaluation ("can A talk to B?")
  profile/                  profile detection and rules
  procutil/                 starting helper programs without a console window
  prom/                     pod history from Prometheus
  redact/                   hiding Secret values
  schema/                   OpenAPI schemas → form fields
  secrets/                  Secret details, TLS, registry logins, exposure scan
  settings/                 preferences, clamped to safe ranges
  terminal/                 integrated terminal: shells in ConPTY or /dev/ptmx
  trust/                    kubeconfig trust store
  validate/                 input validation
  vault/                    encrypted local vault
  version/                  version string, user agent
frontend/
  src/pages/                one file per page
  src/components/           shared UI parts
  src/state/                settings, clusters, navigation, forwards
  src/lib/                  API re-exports and pure helpers (with tests)
  bindings/                 generated by Wails, never edited by hand
build/                      Wails build tasks, icons, Windows manifest, Linux packaging (nfpm, AppImage)
scripts/                    release helpers: version stamping, collecting Linux packages
docs/                       this document, the user guide, testing guides, progress, test data
.github/workflows/          CI, CodeQL and releases
```
