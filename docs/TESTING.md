# Testing Kupi by hand

A checklist for trying every feature before a release. Automated tests cover most of the logic (`go test ./...`, `wails3 task test:integration`, `cd frontend && npm test`); this list is about using the real app.

## Setup

1. Start Docker, then create the test cluster (API on 127.0.0.1:6550, your kubectl context is not switched):

   ```bash
   wails3 task cluster:up
   ```

2. Load the test data:

   ```bash
   kubectl --context k3d-kupi-test apply -f docs/testdata/demo-workloads.yaml
   kubectl --context k3d-kupi-test apply -f docs/testdata/gitops-fixtures.yaml
   kubectl --context k3d-kupi-test apply -f docs/testdata/gitops-fixtures.yaml
   KUPI_DEMO_RELEASE=1 go test -tags integration -run TestInstallDemoRelease ./internal/kube/
   ```

   The GitOps fixtures are applied twice on purpose: their custom resource types must exist before the sample apps. The last line installs the Helm release `kupi-demo/demo-web` with two revisions.

3. Run the app: `wails3 dev`. To keep this run's settings, audit log and vault apart from your real ones, set `KUPI_CONFIG_DIR` to an empty folder first.

If Go runs out of memory while building (the Helm SDK is large), add `-p 2` to `go build` and `go test`.

## What only a person can check

These have never been exercised by the automated runs:

- [ ] **The native window** on Windows (WebView2) and Linux (WebKitGTK): everything below was checked in a browser, not in the window.
- [ ] **Copying a Secret value** (Secrets page, Copy): paste it somewhere, wait 30 seconds, paste again; it should be gone. On Windows, press Win+V: the value should not be in clipboard history.
- [ ] **Linux build and the vault on Linux**, in WSL2 Ubuntu or a Linux desktop: create a vault with the system password store (GNOME Keyring or KWallet), lock it, unlock it.

## Checklist

### Clusters and safety

- [ ] Clusters page lists your kubeconfig contexts with a profile (local, dev, staging, production). `k3d-kupi-test` shows as local.
- [ ] A context you rename to contain "prod" is treated as production: read-only until you switch editing on, and switching on asks for a reason.
- [ ] Every change opens a review with a diff first; on production it needs a reason, and deletes need the name typed.
- [ ] Audit log: every change and every Secret reveal is listed; "Verify" reports the chain intact.

### Everyday work

- [ ] Resources: browse Pods, Deployments and a custom resource type; columns match `kubectl get`.
- [ ] Logs: open the `ticker` Deployment's logs; lines from both pods stream, filter works.
- [ ] Shell: open a shell in a `ticker` pod; type `ls`.
- [ ] Create or edit: create a Deployment from the form, switch to YAML and back, review, apply.
- [ ] Port forwards: forward a Service, open it in the browser, delete the pod behind it; the forward reconnects on the same port.
- [ ] Overview: node and pod CPU and memory.
- [ ] Map: Ingress → Service → Deployment → ConfigMap/Secret edges; health and security overlays.

### Images and local clusters

- [ ] Images: search Docker Hub, pull a tag, load it into `kupi-test`, deploy it.
- [ ] Local clusters: create a small cluster, stop it, start it, delete it (typed confirmation).
- [ ] Laptop mode (Local clusters → Laptop mode): memory and CPU use shows; apply a 2048 MB / 1.5 CPU cap and lift it again; set "Stop when idle" to 15 minutes, leave Kupi alone for 15 minutes, and see the cluster stop; connect to it and it starts again.

### Secrets and vault

- [ ] Secrets: `demo-credentials` shows its keys but no values, and a warning that its values are in an annotation. Reveal shows a value for 30 seconds.
- [ ] Edit a Secret: replace one key, add one, remove one; the review shows key names only.
- [ ] New TLS Secret from a certificate and key; "Check certificate" shows the names and expiry.
- [ ] Exposure scan finds the annotation problem on `demo-credentials`.
- [ ] Vault: create it, add an item by importing a `.env` file, reveal a value, create a Secret from it, lock and unlock.

### History, incidents, GitOps, Helm

- [ ] Incident: the crash-looping `crasher` pod is listed as critical with its log line "fatal: config missing".
- [ ] History: change a ConfigMap with `kubectl edit`; it appears as "changed by kubectl edit" with a diff. The object's own History tab shows it too.
- [ ] GitOps: `shop` (Argo CD, out of sync) and `platform` (Flux, ready). Suspend `platform`, then Resume. The `ticker` Deployment shows a "Managed by Argo CD" banner.
- [ ] Helm: `demo-web` shows two revisions; the manifest hides `adminPassword`; "Show and edit values" asks nothing on local clusters and is in the audit log. Change `replicas`, review the upgrade (diff shows only replicas), apply. Roll back to revision 1. Uninstall.

## Cleaning up

```bash
kubectl --context k3d-kupi-test delete -f docs/testdata/gitops-fixtures.yaml
wails3 task cluster:down
```
