# Kupi user guide

Kupi (Kubernetes Pilot) is a desktop app for looking after Kubernetes clusters, from a small cluster on your own computer to production. This guide explains where everything is and what everything does.

Contents:

1. [Before you start](#1-before-you-start)
2. [The window at a glance](#2-the-window-at-a-glance)
3. [How Kupi keeps you safe](#3-how-kupi-keeps-you-safe)
4. [Clusters](#4-clusters)
5. [Browsing resources and the detail panel](#5-browsing-resources-and-the-detail-panel)
6. [Overview](#6-overview)
7. [Map](#7-map)
8. [Incident](#8-incident)
9. [History](#9-history)
10. [Helm](#10-helm)
11. [GitOps](#11-gitops)
12. [Secrets](#12-secrets)
13. [Vault](#13-vault)
14. [Images](#14-images)
15. [Local clusters and laptop mode](#15-local-clusters-and-laptop-mode)
16. [Port forwards](#16-port-forwards)
17. [Create or edit](#17-create-or-edit)
18. [Audit log](#18-audit-log)
19. [Keyboard and command palette](#19-keyboard-and-command-palette)
20. [Where Kupi keeps its files](#20-where-kupi-keeps-its-files)
21. [Troubleshooting](#21-troubleshooting)

---

## 1. Before you start

Kupi manages clusters you can already reach with `kubectl`. It reads the same kubeconfig file (`%UserProfile%\.kube\config` on Windows, `~/.kube/config` on Linux, or the files in `KUBECONFIG`).

You need at least one of:

- **A cluster on your computer.** Install Docker Desktop and k3d, and Kupi can create clusters for you (see [Local clusters](#15-local-clusters-and-laptop-mode)).
- **An existing cluster** (cloud or company), with its context in your kubeconfig.

Kupi itself needs nothing else: it has its own Kubernetes client and its own Helm, so `kubectl` and `helm` are optional. On Windows it uses the Microsoft Edge WebView2 runtime, which Windows 11 includes.

---

## 2. The window at a glance

```
┌─────────────┬──────────────────────────────────────────────────────────────┐
│ K Kupi  Ctrl K│ [cluster ▾] [Local]  v1.35.5  [All namespaces ▾]  Editing on │  ← context bar
│             ├──────────────────────────────────────────────────────────────┤
│ Clusters    │                                                              │
│ Overview    │                                                              │
│ Map         │                    the page you opened                       │
│ Incident    │                                                              │
│ History     │                                          ┌───────────────────┤
│ Helm        │                                          │  detail panel     │
│ GitOps      │                                          │  (when you click  │
│ Secrets     │                                          │   an object)      │
│ Vault       │                                          │                   │
│ Images      │                                          │                   │
│ Local clusters                                         │                   │
│ Port forwards                                          │                   │
│ Create or edit                                         │                   │
│ Audit log   │                                          │                   │
│ ─────────── │                                          │                   │
│ Filter types│                                          │                   │
│ ▾ Workloads │                                          │                   │
│   Pods …    │                                          │                   │
│ ▸ Network   │                                          │                   │
└─────────────┴──────────────────────────────────────────┴───────────────────┘
```

- **Sidebar (left).** The top part lists Kupi's pages. Pages that need a cluster (Overview, Map, Incident, History, Helm, GitOps, Secrets) appear once you are connected. Below them is every resource type the connected cluster serves, grouped into Workloads, Network, Config, Storage, Access control, Cluster and Custom resources. Type in "Filter types" to find one (short names like `deploy` or `svc` work).
- **Context bar (top).** The cluster you're working on, its profile (Local, Dev, Staging or Production), its Kubernetes version, the namespace in view ("All namespaces" or one namespace), and the editing switch.
- **Main area.** The page you opened.
- **Detail panel (right).** Opens when you click an object in a list or on the map.

---

## 3. How Kupi keeps you safe

### Profiles

Each cluster has a **profile**: Local, Dev, Staging or Production. Kupi guesses it from the cluster's name and address (k3d, kind, minikube and Docker Desktop clusters are Local; names with "prod" or "live" are Production). **A cluster Kupi can't recognise is treated as Production.** You can change the profile on the Clusters page.

| | Local, Dev, Staging | Production |
|---|---|---|
| Editing | on | off until you switch it on, with a reason |
| Reason for each change | no | yes, saved in the audit log |
| Typing the name to delete | no | yes |
| Revealing a Secret value | no reason needed | reason needed |
| Namespace when you connect | all namespaces | your default namespace, to keep load low |

### The editing switch

On the right of the context bar: **Switch to read-only** turns changes off for this cluster; **Switch editing on** turns them back on (production asks for a reason). Read-only lasts until you disconnect.

### The review dialog

Nothing changes a cluster without a review. Every button that changes something (Apply, Scale, Restart, Delete, Edit a Secret, Helm upgrade and so on) first opens the **review dialog**:

- **What changes:** a plain-language summary.
- **Warnings:** for example "an autoscaler manages this workload's copies", "managed by Helm: the next helm upgrade will undo this", "managed by Argo CD".
- **Diff:** the object before and after, with Secret values shown only as "(changed)" or "(unchanged)".
- **Reason** (production) and **type the name** (deletes on production).

The cluster has already checked the change in a dry run, including your permissions and the cluster's own policies, so if it would fail, the dialog tells you why and the button is disabled. The preview is valid for 5 minutes, and if someone else changes the object in the meantime, Kupi refuses to overwrite it and asks you to review again.

### The audit log

Every change, failed change, shell session and Secret reveal is recorded, see [Audit log](#18-audit-log).

---

## 4. Clusters

**Where:** sidebar → Clusters.

The list shows every context in your kubeconfig with its cluster, user, server and profile.

- **Connect** opens a session. The first time you connect to a context that uses a **credential plugin** (a program that logs you in, for example `aws`, `gcloud` or `kubelogin`), Kupi shows the exact command, its arguments and the names of the environment variables it sets, and asks you to **trust this file**. Kupi asks again if the file ever changes.
- **Disconnect** closes the session and stops its logs, shells, watches, metrics and port-forwards.
- **Profile** lets you override the detected profile, or go back to automatic detection.
- When several clusters are connected, pick the one you're working on in the context bar's cluster menu.

A warning appears when a context skips TLS certificate checks (`insecure-skip-tls-verify`): anyone between you and the cluster could read or change your traffic.

---

## 5. Browsing resources and the detail panel

**Where:** sidebar → any resource type (for example Workloads → Deployments).

### The list

- Columns are the ones `kubectl get` shows for that type, including the extra columns custom resources define.
- The list updates by itself when the cluster changes.
- **Filter rows** searches names and cells. **Warnings only** shows rows that need attention. Click a column header to sort.
- The namespace selector in the context bar decides which namespace you see.
- "Copy as kubectl" buttons copy the equivalent `kubectl` command.

### The detail panel

Click a row to open it. The tabs depend on the type:

| Tab | What it shows |
|---|---|
| Overview | Labels, owner ("Controlled by"), creation time, UID; containers and their state for pods |
| Metrics | Pods only: CPU and memory over the last hour, with request and limit lines (needs metrics-server) |
| YAML | The object as YAML, read-only, without noise fields. **Edit with form or YAML** opens it in the editor. For Secrets: values hidden, with **Open in Secrets** |
| Events | Kubernetes events about the object (Kubernetes keeps them about an hour) |
| History | Changes Kupi recorded for this object: who changed it and what changed (see [History](#9-history)) |
| Logs | Pods and workloads: live logs; for a workload, all its pods merged. Follow, previous container, timestamps, filter, pause |
| Shell | Pods: a terminal inside a container (audited; asks a reason on production) |

Actions at the top of the panel, depending on the type:

- **Scale** (Deployments, StatefulSets): set the number of copies.
- **Restart** (Deployments, StatefulSets, DaemonSets): a rolling restart, like `kubectl rollout restart`.
- **Forward port** (Pods, Services, workloads): see [Port forwards](#16-port-forwards).
- **Delete**.

If Argo CD or Flux manages the object, a yellow **Managed by** banner says so and links to the GitOps page; edits made here would be reverted at the next sync.

---

## 6. Overview

**Where:** sidebar → Overview.

CPU and memory of every node against what it can give to pods, and the pods using the most CPU and memory. It needs **metrics-server** in the cluster (k3d and k3s include it). Kupi samples every 10 seconds while you're looking, keeps one hour, and stops sampling 2 minutes after you leave.

---

## 7. Map

**Where:** sidebar → Map.

A live diagram of how things connect, left to right: Ingress or HTTPRoute → Service → workload → ConfigMap, Secret and storage, and autoscalers → their target.

- Pods are folded into their workload, which shows "3/3 ready", restarts and the worst pod state.
- A **dashed red box** is something that's referenced but doesn't exist, such as a Secret a pod needs or an Ingress backend.
- **Overlays** (top of the map):
  - **Health:** colours by state.
  - **Exposure:** highlights everything reachable from a public entry point.
  - **Security:** flags privileged containers, host network, host paths, containers that may run as root, and no memory limit.
- **Click an object** to focus on its neighbours and see its flags. For a ConfigMap or Secret it lists the workloads a change would affect. The detail panel opens beside the map.
- **Find on the map** searches names.
- With "All namespaces", each namespace is a frame.

---

## 8. Incident

**Where:** sidebar → Incident.

One screen for "what's broken, and what changed right before?". Pick a time window (15 minutes to 24 hours) and press Refresh.

- **Failing now:**
  - pods that crash-loop, can't pull their image, can't be scheduled, ran out of memory or stay not ready;
  - workloads with fewer ready copies than wanted, or a stuck rollout;
  - failed Jobs, unbound volumes and unhealthy nodes;
  - Services with no ready pods behind them.

  Each row has **Open**, which jumps to the object.
- **From the logs:** error lines from up to six failing containers (from the crashed run when a container restarted).
- **What changed:** the changes Kupi recorded and the warning events in the window, newest first. Click a change to see its diff.

It covers the namespace selected in the context bar, or all namespaces.

---

## 9. History

**Where:** sidebar → History, or the History tab of an object.

While Kupi is connected to a cluster, it records every real change to Deployments, StatefulSets, DaemonSets, CronJobs, Services, Ingresses, NetworkPolicies, ConfigMaps, Secrets, autoscalers and volume claims, in every namespace you're allowed to watch.

- **Changes** tab: each change shows what changed, **who changed it** (Kupi, kubectl apply or edit, Helm, Argo CD, Flux, Kubernetes itself…) and when. Click it for the diff. Status updates are ignored, and Secret values are never recorded: a Secret change says "values changed".
- **Warnings** tab: warning events. Kubernetes deletes events after about an hour; Kupi keeps them 14 days.
- **Filters:** kind, exact name, period (last hour to 14 days), and the namespace in the context bar.
- The line at the top says since when Kupi has been recording. Changes made while Kupi was closed aren't known.

---

## 10. Helm

**Where:** sidebar → Helm.

The list shows every Helm release with its chart, revision and status (deployed, failed, pending…). Click a release to see:

| Tab | What it shows |
|---|---|
| History | Every revision with its status, chart version and description. **Roll back to this** on earlier revisions |
| Values | The values the release was installed with. Hidden at first, because they often contain passwords: **Show and edit values** reveals them (audited; production asks a reason) in an editor |
| Manifest | Everything Helm rendered for this revision, with Secret values hidden |
| Resources | The objects the release owns |
| Notes | The chart's notes |
| Chart defaults | The chart's own default values, for reference |

Actions, each with a review dialog:

- **Review upgrade** (Values tab): change values and upgrade with the same chart. The review shows exactly which objects change, after the cluster and the chart's values schema have checked it.
- **Roll back to this** (History tab): returns to an earlier revision. Helm records it as a new revision.
- **Uninstall:** removes the release and its objects (on production, type the name). Volumes created by StatefulSets are kept.

Not available yet: installing new charts and changing a release's chart version.

---

## 11. GitOps

**Where:** sidebar → GitOps.

If the cluster runs **Argo CD** or **Flux**, this page lists their apps: Argo CD Applications, Flux Kustomizations and Flux HelmReleases.

Each app shows:

- whether it matches Git: **Synced** or **Out of sync** for Argo CD, **Ready** or **Not ready** for Flux;
- its health, the source repository and path, the target and applied revision, where it deploys, how many resources it manages, and the last sync;
- **Repository**, which opens the repository in your browser.

Actions (each through the review dialog) ask the tool to act:

- **Argo CD:** **Refresh** compares with Git now; **Sync** syncs to the target revision now.
- **Flux:** **Reconcile** applies from the source now. **Suspend** stops Flux from reconciling, so live edits stay but drift from Git; **Resume** lets it reconcile again.

To change what these tools deploy, change the Git repository: live edits are reverted at the next sync.

---

## 12. Secrets

**Where:** sidebar → Secrets, or **Open in Secrets** from a Secret's YAML tab.

### Looking at a Secret

The left side lists Secrets with their type and number of keys. Click one to see:

- **Keys** with their sizes. Values are never shown unless you ask:
  - **Reveal** shows one value for 30 seconds. On production it asks a reason first, and every reveal is in the audit log.
  - **Copy** puts one value on the clipboard without showing it, and clears the clipboard after 30 seconds. On Windows it's also kept out of clipboard history (Win+V) and cloud sync.
  - **Copy as .env** copies every key as `KEY=value` lines.
- **Certificate details** for TLS Secrets: host names, expiry ("expires in N days"), issuer, whether the private key matches, weak keys.
- **Registries** for image-pull Secrets.
- **Used by:** the workloads, Ingresses and service accounts that use this Secret, and how. Workloads that read it into environment variables only see changes after a restart, so they're marked and have a **Restart** button.
- **Warnings:** "Managed by" (External Secrets, Sealed Secrets, cert-manager, Flux, Argo CD, Helm: change it at the source), and "values copied into an annotation" when `kubectl apply` left them in plain sight.

### Creating and editing

- **New Secret:** choose the namespace, name and type:
  - **Opaque:** any keys.
  - **TLS:** paste or choose the certificate and key files, then **Check certificate**.
  - **Registry login:** registry, user name, password or token.
  - **Basic auth** and **SSH key.**

  **Import .env** fills keys from a `.env` file.
- **Edit:** every existing key starts as **Current value kept**. Choose **Replace** to type a new value, the bin icon to remove a key, or **Add key**. Only what you change is sent, and current values are never loaded into the form.
- Values are masked as you type; the eye icon shows them.

### Exposure scan

The **Exposure scan** tab looks through the namespace (or all namespaces) for credentials stored where they shouldn't be:

- in ConfigMaps;
- in plain environment variables and command-line flags of workloads;
- Secret values copied into annotations.

Each finding says where and how to fix it, never the value.

---

## 13. Vault

**Where:** sidebar → Vault.

The vault keeps credentials **on your computer, encrypted**, so you don't need YAML files with passwords in them. It doesn't need a cluster.

- **Set up:** choose where the key lives:
  - **In the system password store** (Windows Credential Manager, GNOME Keyring or KWallet): unlocks with your login.
  - **Behind a passphrase** of at least 12 characters, asked each time. If you forget it, the vault can't be recovered.
- **Items:** each item has a name, a description and keys. **New item**, **Edit**, and **Import .env** in the editor. **Reveal** and **Copy** work like on the Secrets page.
- **Create Secret:** turns an item into a Kubernetes Secret in the cluster and namespace you pick, through the review dialog. If the Secret exists, the item's keys are added or replaced and its other keys are kept.
- **Lock now**; the vault also locks itself after 15 minutes without use.
- **Delete the vault…** (type "delete my vault") removes the vault and its key for good, for example after a forgotten passphrase.

---

## 14. Images

**Where:** sidebar → Images. Needs Docker Desktop (or Podman) running.

- **Local images:** size, age, whether a container uses it. **Details** shows the layers, exposed ports, user, entrypoint and environment variable *names* (values can hide secrets), and warns when the image runs as root.
- **Search Docker Hub:** Official badges, and a warning for names that imitate official images. **Versions** lists recent tags; **Pull** downloads with live progress.
- **Load into cluster:** copies a local image into a local k3d, kind or minikube cluster, so pods can use it without a registry. Kupi checks the image really arrived on every node.
- **Deploy:** opens the editor with a secure Deployment for the image.
- **Disk usage:** remove untagged images, unused images, or the build cache.

---

## 15. Local clusters and laptop mode

**Where:** sidebar → Local clusters. Needs Docker Desktop and k3d.

- **New cluster:** name, number of agents (0 to 5) and, optionally, a k3s version. The API listens on 127.0.0.1 only, and your current kubectl context isn't switched.
- **Connect**, **Start**, **Stop** (frees its memory and CPU), and delete with the bin icon (type the name).

### Laptop mode

Press **Laptop mode** on a cluster:

- **Usage:** how much memory and CPU the cluster takes, per node, as a share of the machine. With Docker Desktop, "the machine" is what Docker's virtual machine is allowed to use. **Measure again** refreshes it.
- **Caps:** a memory cap (MB, at least 768) and a CPU cap (at least 0.5) per node; **Apply caps**. Leave both empty and apply to lift the caps. Caps stay when the cluster restarts.
- **Stop when idle:** Never, or after 15 minutes to 4 hours. Kupi stops the cluster after that long without using it; open port-forwards count as use, tools outside Kupi don't. When you next connect to it, Kupi starts it again.

---

## 16. Port forwards

**Where:** a Pod, Service or workload → **Forward port**; manage them in sidebar → Port forwards.

- Choose the remote port and a local port (or let Kupi pick one). The forward listens on **127.0.0.1 only**, so other computers on your network can't reach it.
- If the pod restarts or is replaced, the forward reconnects to a new ready pod **on the same local port**.
- The Port forwards page lists them all, with **Open** (in your browser), **Copy** (the address) and **Stop**. The sidebar shows how many are running.

---

## 17. Create or edit

**Where:** sidebar → Create or edit, or **Edit with form or YAML** on an object.

- **Start from:** pick a secure template (Deployment, Service, ConfigMap, Namespace, Job, CronJob, PersistentVolumeClaim), or paste or write YAML from scratch.
- **Form and YAML** edit the same object; switch at any time. The form comes from the cluster's own schema, so it works for custom resources too. Important fields come first, and **All fields**, with search, shows everything.
- Help text comes from the schema. Checks warn when a Deployment's selector doesn't match its pod labels, and show how many pods a Service would select.
- **Preview change** opens the review dialog. Kupi uses server-side apply. If another tool manages some of the fields you changed, the review lists the conflicts, and **Take over these fields and preview again** is your explicit choice.
- Secrets are edited on the Secrets page, not here.

---

## 18. Audit log

**Where:** sidebar → Audit log.

Every change Kupi made (or tried to make), with the time, cluster, profile, your computer user, your Kubernetes user, the resource, the reason and the outcome. Reveals of Secret values, Helm values, shells, port-forwards, trust and profile changes, and local cluster actions are recorded too.

**Verify** checks the chain: each entry contains the fingerprint of the previous one, so a changed or deleted line is detected. The log never contains Secret values or diffs, only a fingerprint of each diff.

---

## 19. Keyboard and command palette

- **Ctrl+K** opens the command palette: jump to any page, any resource type, or connect to a cluster by typing part of its name.
- **Esc** closes dialogs and the palette.
- Every control can be reached with Tab, and dialogs keep focus inside them.

---

## 20. Where Kupi keeps its files

In your user config folder: `%AppData%\kupi` on Windows, `~/.config/kupi` on Linux. Set the environment variable `KUPI_CONFIG_DIR` to use another folder (useful for testing). Only your user can read these files.

| File | What it is |
|---|---|
| `audit.log` | Audit log |
| `trust.json` | Kubeconfig files you trusted |
| `profiles.json` | Profile overrides |
| `history/` | Recorded changes and warnings (14 days) |
| `vault.json`, `vault.age`, `vault-key.age` | The vault (encrypted) |
| `laptop.json` | Laptop-mode settings |

Kupi never changes your kubeconfig.

---

## 21. Troubleshooting

| Problem | What to do |
|---|---|
| Kupi doesn't open on Windows | Install the Microsoft Edge WebView2 Runtime from Microsoft's site |
| Windows says "Windows protected your PC" | The test build isn't signed. Choose **More info → Run anyway** if you trust where it came from |
| Connecting to a k3d cluster times out on Windows | The kubeconfig points at `host.docker.internal`, which can resolve to an old address. Kupi's error says which port to use: change the server to `https://127.0.0.1:<port>`, or create clusters from Kupi's Local clusters page, which binds to 127.0.0.1 |
| "Trust this kubeconfig first" | The context uses a credential plugin. Open Clusters, check the command shown, and trust the file |
| Overview says metrics aren't available | Install metrics-server in the cluster |
| Images page says no container engine | Start Docker Desktop (or Podman) |
| A change's button is disabled in the review | Read the reason shown: editing off, no permission, or the cluster rejected the dry run |
| "This change preview has expired or was already used" | Previews last 5 minutes and run once. Prepare the change again |
| "The release changed since this preview" | Someone upgraded the Helm release meanwhile. Review again |
| A Helm upgrade fails with "conflict … with kubectl" | Someone edited one of the release's objects by hand. Undo that edit, or change the value in the release |
