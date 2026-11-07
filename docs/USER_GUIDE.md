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
18. [Terminal](#18-terminal)
19. [Settings](#19-settings)
20. [Audit log](#20-audit-log)
21. [Keyboard and command palette](#21-keyboard-and-command-palette)
22. [Where Kupi keeps its files](#22-where-kupi-keeps-its-files)
23. [Troubleshooting](#23-troubleshooting)

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
┌────────────────┬───────────────────────────────────────────────────────────┐
│ K› KUPI  Ctrl K│ [cluster ▾] [Local]  v1.35.5  [All namespaces ▾]  Editing on│  ← context bar
│ CLUSTERS       ├───────────────────────────────────────────────────────────┤
│  Clusters      │                                                           │
│  Overview      │                                                           │
│  Map           │                 the page you opened                       │
│  Incident      │                                                           │
│  History       │                                       ┌───────────────────┤
│  Helm          │                                       │  detail panel     │
│  GitOps        │                                       │  (when you click  │
│  Secrets       │                                       │   an object)      │
│ WORKSPACE      │                                       │                   │
│  Create or edit│                                       │                   │
│  Terminal      │                                       │                   │
│  Port forwards │                                       │                   │
│  Images        │                                       │                   │
│  Local clusters│                                       │                   │
│  Vault         │                                       │                   │
│ ────────────── │                                       │                   │
│ Filter types   │                                       │                   │
│ ▾ Workloads …  │                                       │                   │
│ Audit  Settings│                                       │                   │
└────────────────┴───────────────────────────────────────┴───────────────────┘
```

- **Sidebar (left).** Kupi's pages, in two groups. **Clusters** holds the cluster list and the pages that need a connected cluster (Overview, Map, Incident, History, Helm, GitOps, Secrets), which appear once you're connected. **Workspace** holds the tools that work on their own: Create or edit, Terminal, Port forwards, Images, Local clusters and Vault. Below them is every resource type the connected cluster serves, grouped into Workloads, Network, Config, Storage, Access control, Cluster and Custom resources. Type in "Filter types" to find one (short names like `deploy` or `svc` work). The **Audit** and **Settings** buttons sit at the bottom, next to the version.
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

Every change, failed change, shell session and Secret reveal is recorded, see [Audit log](#20-audit-log).

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
| Metrics | Pods only: CPU and memory with request and limit lines. The last hour comes from metrics-server; when the cluster runs Prometheus, **6 hours**, **24 hours** and **7 days** come from it |
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
- **Pods of a workload.** Click a Deployment, StatefulSet, DaemonSet, ReplicaSet, Job, CronJob or Service, and its pods fan out under it as small cards (failing ones first, with their status, readiness and restarts). The side panel lists every pod with its node and age. Click a pod, on the map or in the list, for its logs, shell, events and metrics; **Back to …** returns to the workload. Settings → Map can turn the cards off.
- **Find on the map** searches names.
- With "All namespaces", each namespace is a frame.
- **Check traffic** answers "can A talk to B?" from the cluster's NetworkPolicies. Pick where the traffic starts (a pod or workload), where it goes (a Service, pod or workload), a port (0 for any) and the protocol, then **Check**. Kupi picks a real pod on each side, follows a Service port to the pod port, and evaluates both directions: **leaving the source** (its egress policies) and **entering the destination** (its ingress policies). The answer is **Allowed** or **Blocked**, with the policies that decided it, and a dashed green or red line joins the two on the map. It reads the policies; it doesn't send traffic. NetworkPolicies only work when the cluster's network plugin enforces them.
- **Replay** steps through the changes [History](#9-history) recorded (last hour, 24 hours or 7 days) on the map, oldest first. Each step highlights the object that changed and says what changed it and when. Use the arrows or the slider, or **Play** to go through them one every 1.5 seconds.

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
| Chart version | Upgrade the release to another version of its chart, or to another chart |

Actions, each with a review dialog:

- **Review upgrade** (Values tab): change values and upgrade with the same chart. The review shows exactly which objects change, after the cluster and the chart's values schema have checked it.
- **Chart version** tab: Helm releases don't record where their chart came from, so find the chart (the search starts with the chart's name, see [Installing a chart](#installing-a-chart)) and pick a version. The release keeps its own values: **Load the current values** shows them (audited, like the Values tab) so you can edit them for the new version. Then review the upgrade like above.
- **Roll back to this** (History tab): returns to an earlier revision. Helm records it as a new revision.
- **Uninstall:** removes the release and its objects (on production, type the name). Volumes created by StatefulSets are kept.

### Installing a chart

**Install a chart** (top of the page) finds a chart in one of two ways:

- **Search Artifact Hub:** type a name; results show the repository, the latest version and a badge for verified publishers. Only your search text goes to artifacthub.io.
- **Repository address:** an `https://` chart repository (`https://charts.example.com`) or an OCI registry (`oci://ghcr.io/org/charts`), and the chart name. Plain `http://` is refused.

Then choose a version (the newest is picked; deprecated versions are marked), a release name and namespace, and edit the values, which start as the chart's defaults. **Review install** renders the chart and dry-runs it on the cluster, so the review lists every object it would create. It warns about cluster-wide objects (CRDs, ClusterRoles, webhooks) and about hooks, which Helm runs during the install and the dry run can't preview. Kupi checks each chart download against the digest in the repository's index.

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

### Proposing a change as a pull request

When you edit an object that Argo CD or Flux manages (Create or edit, or **Edit with form or YAML**), a yellow notice says so. If the app's source is a GitHub repository, **Propose in Git** finds the file under the app's path that holds the object, puts your edited object in its place (other objects and comments in the file are kept), and shows the diff. **Open pull request** creates a branch named `kupi/…`, commits the file and opens the pull request; its link appears when it's done. The cluster itself isn't touched: the change arrives when the pull request is merged and the tool syncs.

- It needs a GitHub token, saved once in the **GitHub** box on the GitOps page and kept encrypted in your [vault](#13-vault) (so the vault must be unlocked). Use a fine-grained token with read and write access to the repository's contents and pull requests, or a classic token with the `repo` scope.
- Secrets are refused: they don't belong in Git as plain text.
- Only GitHub is supported for now. Kustomize patches and Helm values aren't edited: Kupi needs the object written out as plain YAML in the repository.

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
- **Registry logins** (tab): user names and passwords or tokens for private registries, kept encrypted in your [vault](#13-vault). Kupi uses the matching login when it pulls from that registry; pulls from other registries are anonymous. The vault must be unlocked for a login to be used; if a pull fails while it's locked, the message says so.
- **Security** (in an image's details):
  - **Scan for vulnerabilities** runs [Trivy](https://trivy.dev) on your computer and lists known vulnerabilities, worst first, with the fixed version when there is one.
  - **Check signature** runs [cosign](https://www.sigstore.dev) to check the image's Sigstore signature, and shows who signed it. A valid signature only means *someone* signed it: check that the signer is the publisher you expect.
  - Both tools are optional; install them and reopen the details (on Windows: `winget install --id AquaSecurity.Trivy -e` and `winget install --id Sigstore.Cosign -e`, then restart Kupi). Nothing is uploaded by Kupi. The first scan downloads Trivy's vulnerability database, which takes a few minutes.

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
- **Open file** loads a `.yaml`, `.yml` or `.json` file from your computer (up to 1 MB) into the YAML view. Kupi reads only the file you pick.
- **Several objects at once.** A manifest can hold many objects separated by `---` (or a `kind: List`), like most manifests you copy from documentation. The page shows how many it holds. **Preview change** checks each object on its own (permissions, dry run, diff) and the review dialog lists them in the order they'll be applied, the same order Helm uses: namespaces, config, storage, roles and Services first, then workloads, then Ingresses. Click an object to see its diff. One confirmation applies them all, and each gets its own audit entry.
  - Objects in a namespace the same manifest creates, or of a type its CustomResourceDefinition defines, show **Checked when applied**: the cluster can only check them once the namespace or type exists. Kupi waits for a new type to be ready before applying objects of that type.
  - If an object fails, Kupi stops and tells you which ones before it were applied (Kubernetes has no all-or-nothing apply).
- **Form and YAML** edit the same object; switch at any time. The form edits one object, so with a multi-object manifest use the YAML view. The form comes from the cluster's own schema, so it works for custom resources too. Important fields come first, and **All fields**, with search, shows everything.
- **Suggestions.** Fields that name another object offer the ones that exist: type a name, or open the list. This covers the namespace, ConfigMaps and Secrets (and their key names, never values) used by environment variables and volumes, ServiceAccounts, PersistentVolumeClaims, storage, ingress and priority classes, nodes, the Service behind an Ingress, an autoscaler's target, and RBAC roles and subjects. A name that doesn't exist yet gets a gentle warning, since the same manifest may create it.
- **Sliders.** CPU, memory and storage requests and limits, and replica counts, have a slider for quick changes next to a box for the exact value (`250m`, `1.5`, `512Mi`, `2Gi`…). The box shows what the value means ("0.25 CPUs", "512 MiB"), and a limit below its request is flagged before the cluster rejects it.
- Help text comes from the schema. Checks warn when a Deployment's selector doesn't match its pod labels, and show how many pods a Service would select.
- **Preview change** opens the review dialog. Kupi uses server-side apply. If another tool manages some of the fields you changed, the review lists the conflicts, and **Take over these fields and preview again** is your explicit choice.
- Secrets are edited on the Secrets page, not here.

---

## 18. Terminal

**Where:** sidebar → Terminal, or **Ctrl+`** (press it again to go back to where you were).

Your own shell inside Kupi: PowerShell, Command Prompt, Git Bash or WSL on Windows; bash, zsh or fish on Linux. Use kubectl, helm, docker, k3d or anything else the usual way.

**It is off until you turn it on.** The first time, the page explains what the terminal can do and **Turn on the terminal…** opens a system dialog to confirm. Commands typed in the terminal run directly on your computer and clusters: they don't go through Kupi's preview, confirmation or audit log. Opening a terminal is recorded in the audit log, and Settings → Terminal → **Turn off** closes every shell.

What it adds to a plain terminal:

- **kubectl follows Kupi.** Each new tab uses the cluster and namespace you're connected to in Kupi, shown under the tabs, with the profile badge. Your kubeconfig's current context is never changed: Kupi puts a small file in front of your kubeconfig that only picks the context and namespace (it holds no credentials), so even `kubectl config use-context` inside the tab only affects that tab. `KUPI_CONTEXT`, `KUPI_NAMESPACE` and `KUPI_PROFILE` are set for your prompt or scripts.
- **Production stands out.** A tab on a production cluster has a red bar saying commands skip Kupi's preview.
- **Paste check.** A paste with a line break would run straight away in most shells, so Kupi shows it first and offers **Paste**, **Paste as one line, don't run**, or Cancel. On production, pasted commands that delete, drain, scale to zero, roll back or uninstall are highlighted in red. You can switch the check off in Settings.
- **Tabs.** **+** opens a tab (the arrow next to it picks another shell). Tabs keep running while you use other pages. **Ctrl+Shift+T** opens a tab, **Ctrl+Shift+W** closes one, middle-click closes too. When a shell ends, press Enter to start a new one.
- **Snippets** (lightning icon): ready-made commands for Kubernetes, Helm, Docker and k3d, plus your own from Settings. Picking one types it at the prompt with placeholders such as `NAME` for you to fill in; nothing runs until you press Enter.
- **Find** (**Ctrl+Shift+F**) searches the output; Enter and Shift+Enter move between matches.
- **Tools** (wrench icon) lists which of kubectl, helm, docker, k3d, kind, minikube, flux and argocd are installed, with versions, and warns when your kubectl is more than one minor version away from the cluster.
- **Copy:** select text and press **Ctrl+C** (without a selection Ctrl+C interrupts, as usual) or **Ctrl+Shift+C**. **Copy all output** and **Clear** are in the toolbar.
- **Links** in the output can be clicked; Kupi asks before opening them in your browser.

---

## 19. Settings

**Where:** the **Settings** button at the bottom of the sidebar.

| Section | What you can set |
|---|---|
| Appearance | Theme (System, Dark, Light) and interface size (90 to 150%) |
| Logs | How many lines to load first, and whether to show timestamps |
| Map | How often it refreshes, and whether a selected workload's pods fan out under it |
| Security and privacy | How soon copied secrets are cleared (5 minutes at most), how soon the vault locks itself (8 hours at most), how long change history is kept (1 to 90 days), and a link to the audit log |
| Terminal | On or off, the shell, whether kubectl follows Kupi's cluster, the paste check, copy on select, font size, scrollback, and your own snippets |
| Clusters and credentials | The kubeconfig files Kupi reads, the credential plugins you trusted (with **Revoke**), and a link to set cluster profiles |
| About | Version, license, and where Kupi keeps its files |

Settings are saved on this computer only. Safety limits apply even if someone edits `settings.json` by hand, and changes to safety settings are written to the audit log. **Reset all** returns everything to its defaults and turns the terminal off; your snippets stay.

---

## 20. Audit log

**Where:** the **Audit** button at the bottom of the sidebar, or Settings → Open the audit log.

Every change Kupi made (or tried to make), with the time, cluster, profile, your computer user, your Kubernetes user, the resource, the reason and the outcome. Reveals of Secret values, Helm values, shells, port-forwards, trust and profile changes, and local cluster actions are recorded too.

**Verify** checks the chain: each entry contains the fingerprint of the previous one, so a changed or deleted line is detected. The log never contains Secret values or diffs, only a fingerprint of each diff.

---

## 21. Keyboard and command palette

- **Ctrl+K** opens the command palette: jump to any page, any resource type, or connect to a cluster by typing part of its name.
- **Ctrl+`** opens the terminal, and goes back on a second press. In the terminal: **Ctrl+Shift+T** new tab, **Ctrl+Shift+W** close tab, **Ctrl+Shift+F** find, **Ctrl+Shift+C** copy.
- **Esc** closes dialogs and the palette.
- Every control can be reached with Tab, and dialogs keep focus inside them.

---

## 22. Where Kupi keeps its files

In your user config folder: `%AppData%\kupi` on Windows, `~/.config/kupi` on Linux. Set the environment variable `KUPI_CONFIG_DIR` to use another folder (useful for testing). Only your user can read these files.

| File | What it is |
|---|---|
| `audit.log` | Audit log |
| `trust.json` | Kubeconfig files you trusted |
| `profiles.json` | Profile overrides |
| `history/` | Recorded changes and warnings (14 days unless changed in Settings) |
| `vault.json`, `vault.age`, `vault-key.age` | The vault (encrypted) |
| `laptop.json` | Laptop-mode settings |
| `settings.json` | Your preferences and terminal snippets |
| `terminal/` | While a terminal tab is open: the small file that points its kubectl at Kupi's cluster (no credentials). Removed when the tab closes |

Kupi never changes your kubeconfig.

---

## 23. Troubleshooting

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
