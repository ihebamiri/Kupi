# Testing Kupi on Windows, step by step

Follow this from top to bottom. It starts with installing what Kupi needs, then creates a small test cluster with sample data, then walks through every feature with what you should see. Tick each box as you go and note anything that looks wrong.

You don't need Go, Node or any development tool: only the Kupi app and the tools in Part 1. The [user guide](USER_GUIDE.md) explains each screen in more detail.

Time needed: about 30 minutes for setup, 1 to 2 hours for the tests.

---

## Part 1. Install the prerequisites

### What you need

| Tool | Required? | Why | Tested version |
|---|---|---|---|
| Windows 11 (64-bit) | yes | Windows 10 22H2 should work but wasn't tested | Windows 11, build 26200 |
| Microsoft Edge WebView2 Runtime | yes | Draws Kupi's window (Windows 11 already has it) | 156.0.4314.58 |
| Docker Desktop | yes, for these tests | Runs the local Kubernetes cluster, and the Images page | 4.86.0 (Docker Engine 29.7.2) |
| k3d | yes, for these tests | Creates the local Kubernetes cluster inside Docker | 5.9.0 (Kubernetes v1.35.5, k3s) |
| kubectl | yes, for these tests | Loads the test data from the command line (Kupi itself doesn't need it) | 1.36.1 |
| Helm | yes, for these tests | Installs the demo Helm release (Kupi has Helm built in and doesn't need it) | any 3.x or 4.x |
| Trivy | optional | Image vulnerability scans (L6), from [trivy.dev](https://trivy.dev) | 0.76.2 |
| cosign | optional | Image signature checks (L7), from [sigstore.dev](https://docs.sigstore.dev/cosign/system_config/installation/) | 3.1.4 |
| A GitHub account | optional | Pull requests for GitOps-managed objects (K4) | |

Trivy and cosign install with winget (`winget install --id AquaSecurity.Trivy -e` and `winget install --id Sigstore.Cosign -e`). winget names cosign `cosign-windows-amd64`; Kupi finds it under that name too. Open a new PowerShell window afterwards, so Kupi sees the updated PATH.

Kupi supports Kubernetes 1.34 to 1.37. A computer with 16 GB of memory is comfortable; 8 GB works if few other programs run.

Someone who only wants to use Kupi with an existing cluster needs just Kupi and their kubeconfig file; Docker Desktop, k3d, kubectl and Helm are for local clusters and for these tests.

### Steps

Open **PowerShell** (Start menu → type "PowerShell"). Run each command, then the check below it.

1. **Check WebView2.** Open Settings → Apps → Installed apps and search for "WebView2". If "Microsoft Edge WebView2 Runtime" is missing, install the "Evergreen Bootstrapper" from Microsoft's WebView2 download page.

2. **Install Docker Desktop.**

   ```powershell
   winget install --id Docker.DockerDesktop -e
   ```

   Or download it from docker.com. Restart Windows if asked, start Docker Desktop, accept its terms, and keep the default settings (WSL 2 engine, Linux containers). Wait until it says "Engine running". Docker Desktop is free for personal use, education and small companies.

   Check, which should show a client and server version (29.x or newer):

   ```powershell
   docker version
   ```

3. **Install k3d.**

   ```powershell
   winget install --id k3d.k3d -e
   ```

   Close and reopen PowerShell so it finds the new program. Check, which should show `k3d version v5.9.0` or newer:

   ```powershell
   k3d version
   ```

4. **Install kubectl.**

   ```powershell
   winget install --id Kubernetes.kubectl -e
   ```

   Reopen PowerShell. Check:

   ```powershell
   kubectl version --client
   ```

5. **Install Helm.**

   ```powershell
   winget install --id Helm.Helm -e
   ```

   Reopen PowerShell. Check:

   ```powershell
   helm version
   ```

---

## Part 2. Get Kupi

You receive Kupi as a folder (or a zip to unzip) containing:

```
kupi.exe
docs\USER_GUIDE.md
docs\TESTING-WINDOWS.md        (this file)
docs\testdata\demo-workloads.yaml
docs\testdata\gitops-fixtures.yaml
docs\testdata\multi-object.yaml
docs\testdata\network-policies.yaml
docs\testdata\charts\demo-web\…
```

If you build from the source code instead, the app is `bin\kupi.exe` and the test data is in `docs\testdata\` of the repository.

1. Put the folder somewhere simple, for example `C:\kupi-test`.
2. In PowerShell, go to it. **Every command below runs from this folder.**

   ```powershell
   cd C:\kupi-test
   ```

3. Tell Kupi to use a separate settings folder for testing, so the test leaves your real settings, audit log and vault alone:

   ```powershell
   $env:KUPI_CONFIG_DIR = "$env:TEMP\kupi-test-config"
   ```

   This only lasts for this PowerShell window. Start Kupi from the same window (Part 4).

The test build isn't signed. The first time you run it, Windows may say **"Windows protected your PC"**: choose **More info → Run anyway**.

---

## Part 3. Create the test cluster and the test data

1. **Create the cluster**. This takes about a minute. The API only listens on your computer (127.0.0.1), and your current kubectl context isn't changed.

   If a cluster called `kupi-test` already exists (for example on the developer's PC), delete it first so the test starts clean: `k3d cluster delete kupi-test`.

   ```powershell
   k3d cluster create kupi-test --api-port 127.0.0.1:6550 --wait --kubeconfig-switch-context=false
   ```

   Check, which should show one node `Ready`:

   ```powershell
   kubectl --context k3d-kupi-test get nodes
   ```

2. **Load the sample workloads**: the namespace `kupi-demo` with a log ticker (2 pods), a pod that crashes on purpose (`crasher`), a ConfigMap and a Secret.

   ```powershell
   kubectl --context k3d-kupi-test apply -f docs\testdata\demo-workloads.yaml
   ```

3. **Load the GitOps samples.** These are fake Argo CD and Flux apps with no controller behind them. Run it **twice**: the first run creates the types, the second the apps. An error about "no matches for kind" on the first run is expected.

   ```powershell
   kubectl --context k3d-kupi-test apply -f docs\testdata\gitops-fixtures.yaml
   ```

   ```powershell
   kubectl --context k3d-kupi-test apply -f docs\testdata\gitops-fixtures.yaml
   ```

   Then mark the ticker as owned by the fake Argo CD app:

   ```powershell
   kubectl --context k3d-kupi-test -n kupi-demo label deployment ticker argocd.argoproj.io/instance=shop
   ```

4. **Install the demo Helm release** and upgrade it once, so it has two revisions:

   ```powershell
   helm --kube-context k3d-kupi-test install demo-web docs\testdata\charts\demo-web -n kupi-demo --set adminPassword=s3cret-demo
   ```

   ```powershell
   helm --kube-context k3d-kupi-test upgrade demo-web docs\testdata\charts\demo-web -n kupi-demo --reuse-values --set replicas=2 --set greeting="hello from revision 2"
   ```

   Check, which should show `demo-web` at revision 2:

   ```powershell
   helm --kube-context k3d-kupi-test list -n kupi-demo
   ```

---

## Part 4. Test Kupi

Start Kupi from the same PowerShell window:

```powershell
.\kupi.exe
```

Each step says what to do (**Do**) and what you should see (**Expect**).

### A. First start and connecting

- [ ] **A1.** Do: look at the window. Expect: a window titled "Kupi" (dark or light, following Windows), the Kupi logo at the top of the sidebar, and the Clusters page open.
- [ ] **A2.** Do: find `k3d-kupi-test` in the list. Expect: its profile shows **Local**, and the server is `https://127.0.0.1:6550`.
- [ ] **A3.** Do: press **Connect** on `k3d-kupi-test`. Expect: within a few seconds the context bar shows `k3d-kupi-test`, **Local**, `v1.35.5+k3s1`, "All namespaces" and **Editing on**. The sidebar gains Overview, Map, Incident, History, Helm, GitOps and Secrets, and resource types appear below.
- [ ] **A4.** Do: press **Ctrl+K**, type `pods`, press Enter. Expect: the Pods list opens.

### B. Browsing, logs and shell

- [ ] **B1.** Do: sidebar → Workloads → Pods. Expect: pods from `kube-system` and `kupi-demo`, with the same columns as `kubectl get pods` (Name, Ready, Status, Restarts, Age…). `crasher` shows CrashLoopBackOff or Error.
- [ ] **B2.** Do: in the context bar, choose the namespace `kupi-demo`. Expect: only `kupi-demo` pods.
- [ ] **B3.** Do: type `ticker` in "Filter rows". Expect: only the two ticker pods.
- [ ] **B4.** Do: click a ticker pod. Expect: the detail panel opens on the right with the tabs Overview, Metrics, YAML, Events, Logs and Shell.
- [ ] **B5.** Do: open **Logs**. Expect: new lines arrive every few seconds. Try **Pause**, the filter box and timestamps.
- [ ] **B6.** Do: open **Shell**, type `ls /` and press Enter. Expect: a directory listing. (Kupi records shells in the audit log.)
- [ ] **B7.** Do: open **Metrics** (wait up to a minute for the first samples). Expect: CPU and memory charts.
- [ ] **B8.** Do: sidebar → Workloads → Deployments → `ticker` → **Logs**. Expect: lines from both pods, each prefixed with its pod name.
- [ ] **B9.** Do: on `ticker`, press **Scale**, set 3, continue. Expect: a review dialog: "Scales Deployment kupi-demo/ticker from 2 to 3 copies", a diff `replicas: 2 → 3`, and a warning that Argo CD manages it. Press the apply button. Expect: a success message, and three ticker pods soon after.
- [ ] **B10.** Do: Scale back to 2.
- [ ] **B11.** Do: on `ticker`, look at the top of the panel. Expect: a yellow "Managed by Argo CD Application shop · out of sync…" banner.

### C. Safety and profiles

- [ ] **C1.** Do: in the context bar press **Switch to read-only**, then try **Scale** on `ticker` again. Expect: the review says editing is off, and the apply button is disabled. Press **Switch editing on** afterwards.
- [ ] **C2.** Do: sidebar → Clusters → change the profile of `k3d-kupi-test` to **Production**. Expect: Kupi disconnects it. Connect again. Expect: the context bar shows **Production**, the namespace is a single namespace, and editing is off.
- [ ] **C3.** Do: switch editing on. Expect: it asks for a reason. Give one.
- [ ] **C4.** Do: try to delete the ConfigMap `kupi-demo/demo-settings`. Expect: the review asks for a reason **and** for the name typed exactly. Press **Cancel**: don't delete it.
- [ ] **C5.** Do: set the profile back to automatic detection (or Local) and reconnect. Expect: **Local** again.

### D. Create or edit

- [ ] **D1.** Do: sidebar → Create or edit → Start from **Deployment**. Expect: a form with Name, Namespace, Copies running, Image and more, and the YAML beside it.
- [ ] **D2.** Do: set the name `hello`, namespace `kupi-demo`, image `nginxinc/nginx-unprivileged:1.27-alpine`. Expect: the YAML updates as you type, and switching to YAML and back keeps your values.
- [ ] **D3.** Do: **Preview change**, then apply. Expect: the review shows a new object (all lines added); after applying, `hello` appears in Deployments and its pod becomes Ready.
- [ ] **D4.** Do: open `hello` → YAML → **Edit with form or YAML**, change Copies running to 2, preview. Expect: the diff shows only `replicas: 1 → 2`.
- [ ] **D5.** Do: Create or edit → Start from **Deployment** → click in **Namespace**. Expect: a list of the cluster's namespaces (`default`, `kupi-demo`, `kube-system`…); you can pick one or keep typing your own.
- [ ] **D6.** Do: in the form, look at **Requests** and **Limits**. Expect: CPU and Memory each have a slider and a box with the exact value, and a reading such as "0.1 CPUs" or "128 MiB". Drag the memory **limit** slider below the memory request. Expect: a red line saying the limit is below the request.
- [ ] **D7.** Do: **All fields** → search `configMapRef` → open envFrom → Add item → configMapRef → click **name**. Expect: the ConfigMaps of the chosen namespace are suggested.
- [ ] **D8.** Do: **Open file** → pick `docs\testdata\multi-object.yaml` from the Kupi folder. Expect: the YAML view shows it, the file name appears in the title, and a note says "This manifest holds 4 objects: Service hello, Deployment hello, ConfigMap hello-settings, Namespace kupi-multi".
- [ ] **D9.** Do: **Preview change**. Expect: "Review: 4 objects", listed in this order: Namespace (Create), ConfigMap, Service, Deployment (each "Checked when applied", because their namespace is new). Click one to see its diff. Apply. Expect: "Applied 4 objects", and `kubectl --context k3d-kupi-test -n kupi-multi get all` shows the Deployment with 2 pods and the Service.
- [ ] **D10.** Do: paste a manifest with two `---`-separated objects of your own (for example a ConfigMap and a Secret) into the YAML view and preview it. Expect: both are listed. Cancel.

### E. Overview and Map

- [ ] **E1.** Do: sidebar → Overview. Expect: the node with CPU and memory bars, and the busiest pods.
- [ ] **E2.** Do: sidebar → Map (namespace `kupi-demo`). Expect: boxes for `ticker` (2/2), `demo-web`, `hello` and the failing `crasher` pod; `demo-web` is linked to the ConfigMap `demo-web-config` and the Secret `demo-web-auth`.
- [ ] **E3.** Do: switch the overlay to **Security**. Expect: `crasher` flagged for no memory limit and possibly running as root; `ticker` flagged as possibly running as root; `demo-web` and `hello` unflagged (they run as non-root with limits).
- [ ] **E4.** Do: click `demo-web-config`. Expect: it and `demo-web` are highlighted, and the panel says a change affects `demo-web`.
- [ ] **E5.** Do: click the `ticker` Deployment. Expect: the map moves so that `ticker` and two small pod cards under it are in view (each with its name, "Running · 1/1"), and a **Pods** list above the detail panel shows both with status, ready, restarts, node and age.
- [ ] **E6.** Do: click one of the pod cards. Expect: the panel shows that pod, with Logs, Shell, Events and Metrics tabs, and **← Back to Deployment ticker** returns to the Deployment.
- [ ] **E7.** Do: choose the namespace `kupi-multi` (created in D9) and click the Service `hello`. Expect: the two pods it sends traffic to fan out under it.
- [ ] **E8.** Do: in PowerShell, add two NetworkPolicies: nothing may reach pods in `kupi-multi`, except the `ticker` pods of `kupi-demo` on the `hello` pods' `http` port (8080).

  ```powershell
  kubectl --context k3d-kupi-test apply -f docs\testdata\network-policies.yaml
  ```

- [ ] **E9.** Do: Map, namespace **All namespaces** → **Check traffic**. From **Deployment kupi-demo/ticker**, To **Service kupi-multi/hello**, port `80`, TCP → **Check**. Expect: **Allowed**, `ticker-… → hello-…`, "Leaving the source" allowed (no policy selects it), "Entering the destination" allowed by `hello-from-ticker` (2 policies), the note "Service port 80 goes to pod port 8080", and a dashed green "allowed" line from `ticker` to `hello` on the map.
- [ ] **E10.** Do: From **Pod kupi-demo/crasher**, same destination → Check. Expect: **Blocked**: entering the destination isn't allowed by either policy. The line turns red.
- [ ] **E11.** Do: From `ticker`, To **Deployment kupi-multi/hello**, port `9090` → Check. Expect: **Blocked** (only port 8080 is allowed).
- [ ] **E12.** (Optional: check the answers against real traffic; k3s enforces NetworkPolicies.) Do:

  ```powershell
  kubectl --context k3d-kupi-test -n kupi-demo exec deploy/ticker -- wget -qO- -T 3 http://hello.kupi-multi
  ```

  Expect: the nginx welcome page's HTML. Then from a pod without the `app: ticker` label:

  ```powershell
  kubectl --context k3d-kupi-test -n kupi-demo run probe --rm -i --restart=Never --image=busybox:1.36 -- wget -qO- -T 3 http://hello.kupi-multi
  ```

  Expect: "can't connect to remote host … Connection refused" (k3s rejects blocked connections; other network plugins drop them, and wget says "download timed out").
- [ ] **E13.** Do: close the traffic panel (×), then **Replay** → **last 24 hours**. Expect: "N changes recorded", the newest selected, and the changed object highlighted on the map with what changed it (for example "changed by Kupi"). Use the arrows and the slider, then **Play**: it steps forward every 1.5 seconds and the map follows. Close the replay bar.

### F. Port forwards

- [ ] **F1.** Do: open the `demo-web` Deployment → **Forward port**, remote port 8080, local port empty. Expect: a forward on `127.0.0.1:<port>`.
- [ ] **F2.** Do: sidebar → Port forwards → **Open**. Expect: your browser shows the nginx welcome page.
- [ ] **F3.** Do: delete the pod behind it (Pods → `demo-web-…` → Delete), wait 20 seconds, and reload the browser page. Expect: it still works on the **same** port, and the forward shows "reconnected".
- [ ] **F4.** Do: **Stop** the forward.

### G. Secrets

- [ ] **G1.** Do: sidebar → Secrets → `demo-credentials`. Expect: the key `password` with its size, no value, and a red warning that `kubectl apply` copied the values into an annotation.
- [ ] **G2.** Do: **Reveal**. Expect: the value `not-a-real-password` appears and disappears again after 30 seconds.
- [ ] **G3.** Do: **Copy**, paste it into Notepad. Expect: the value is pasted. Wait 30 seconds and paste again. Expect: nothing is pasted.
- [ ] **G4.** Do: press **Win+V** (enable clipboard history first if Windows asks). Expect: the Secret value is **not** in the history.
- [ ] **G5.** Do: **Edit** → **Add key**, key `api-token`, any value → **Review change**. Expect: the diff lists `api-token: •••••• (changed)` and `password: •••••• (unchanged)`, never a value. Apply.
- [ ] **G6.** Do: **New Secret** → type **TLS certificate and key**. Paste any certificate and key you have, or skip this step. Expect: **Check certificate** shows the host names, expiry and whether the key matches.
- [ ] **G7.** Do: the **Exposure scan** tab → **Scan kupi-demo**. Expect: one finding for `demo-credentials` (values in an annotation), and no values shown.
- [ ] **G8.** Do: open the Secret `demo-web-auth`. Expect: **Used by** lists Deployment `demo-web` ("key adminPassword as environment variable ADMIN_PASSWORD"), marked "Restart picks up changes", with a **Restart** button. Its "Managed by Helm" warning says to change it through the release.
### H. Vault

- [ ] **H1.** Do: sidebar → Vault → keep "In Windows Credential Manager" → **Create vault**. Expect: an empty vault, unlocked.
- [ ] **H2.** Do: **New item**, name `shop-db-dev` → **Import .env** → paste the two lines below → Import → Save. Expect: an item with the keys DB_PASSWORD and DB_USER, values hidden.

  ```
  DB_USER=shop
  DB_PASSWORD='dev-only s3cret'
  ```

- [ ] **H3.** Do: **Reveal** DB_PASSWORD. Expect: `dev-only s3cret`.
- [ ] **H4.** Do: **Create Secret** → namespace `kupi-demo` → Review change → apply. Expect: the new Secret `shop-db-dev` in the Secrets page, with both keys.
- [ ] **H5.** Do: **Lock now**, then **Unlock**. Expect: the item comes back without typing anything.
- [ ] **H6.** (Optional) Do: open Windows Credential Manager → Windows Credentials. Expect: an entry whose name starts with `kupi`, holding the vault key. (When `KUPI_CONFIG_DIR` is set, its name also has a short code, so tests never touch a real vault.)

### I. Incident and History

- [ ] **I1.** Do: sidebar → Incident (all namespaces). Expect: **Critical: Pod kupi-demo/crasher · CrashLoopBackOff…**, the log line `fatal: config missing` under "From the logs", and BackOff warnings under "What changed".
- [ ] **I2.** Do: in PowerShell, change a ConfigMap by hand:

  ```powershell
  kubectl --context k3d-kupi-test -n kupi-demo label configmap demo-settings tested=yes
  ```

  In Kupi: sidebar → History. Expect: "ConfigMap kupi-demo/demo-settings changed by kubectl label", and clicking it shows the label `tested: "yes"` added.
- [ ] **I3.** Do: open the ConfigMap `demo-settings` → **History** tab. Expect: the same change.
- [ ] **I4.** Do: History → **Warnings** tab. Expect: the BackOff warnings for `crasher`.

### J. Helm

- [ ] **J1.** Do: sidebar → Helm. Expect: `demo-web` (kupi-demo, revision 2, deployed), plus `traefik` and `traefik-crd`, which k3s installs itself.
- [ ] **J2.** Do: click `demo-web`. Expect: History with revisions 2 (deployed) and 1 (superseded).
- [ ] **J3.** Do: **Manifest**. Expect: the rendered objects, with `adminPassword: ••••••` and never the real password.
- [ ] **J4.** Do: **Values** → **Show and edit values**. Expect: an editor with `adminPassword: s3cret-demo`, `greeting: hello from revision 2` and `replicas: 2`.
- [ ] **J5.** Do: change `replicas: 2` to `replicas: 3` → **Review upgrade**. Expect: the review's diff shows only `replicas: 2 → 3`. Apply. Expect: revision 3, deployed.
- [ ] **J6.** Do: change replicas to `"lots"` (text) → Review upgrade. Expect: Helm rejects it (the chart's schema wants a number) and nothing changes.
- [ ] **J7.** Do: History → **Roll back to this** on revision 1. Expect: the diff shows `greeting` and `replicas` going back. Apply. Expect: revision 4, deployed.
- [ ] **J8.** Do: in PowerShell, `helm --kube-context k3d-kupi-test history demo-web -n kupi-demo`. Expect: the same four revisions; Kupi's are described "Upgraded by Kupi" and "Rolled back to 1 by Kupi".
- [ ] **J9.** Do: **Install a chart** → Search Artifact Hub for `podinfo` → pick **podinfo** (repository podinfo, by stefanprodan). Expect: its versions, the newest selected, with its description. Choose an **older** version (for example 6.7.0), release name `podinfo`, namespace `kupi-charts`, tick **Create the namespace if it doesn't exist**, keep the values → **Review install**. Expect: a review listing the Deployment and Service it creates. Apply. Expect: `podinfo` in the release list, revision 1, deployed, and its pod Running.
- [ ] **J10.** Do: open `podinfo` → **Chart version** → the search starts with "podinfo"; pick it and the **newest** version → **Load the current values** → Review upgrade. Expect: the diff shows the image tag and chart labels moving to the new version. Apply. Expect: revision 2 with the new chart version.
- [ ] **J11.** Do: Install a chart → **Repository address**: `oci://ghcr.io/stefanprodan/charts`, chart `podinfo` → **Find versions**. Expect: the versions from the registry. Cancel.
- [ ] **J12.** Do: Install a chart → Repository address `http://example.com`, chart `x` → Find versions. Expect: refused, only https is allowed.
- [ ] **J13.** (Optional, about 600 MB of memory: Prometheus for long-range metrics.) Do: Install a chart → Artifact Hub `prometheus` → **prometheus** from prometheus-community. Release name `prometheus`, namespace `monitoring`, create the namespace. In the values, set `enabled: false` under `alertmanager`, `kube-state-metrics`, `prometheus-node-exporter` and `prometheus-pushgateway` → Review install. Expect: warnings that it creates cluster-wide objects (ClusterRole, ClusterRoleBinding). Apply, and wait about 5 minutes. Then open a ticker pod → **Metrics**. Expect: a time range with **6 hours**, **24 hours** and **7 days**; choosing one says "From Prometheus (monitoring/prometheus-server:80)" and draws the minutes Prometheus has collected so far.

### K. GitOps

- [ ] **K1.** Do: sidebar → GitOps. Expect: `shop` (Argo CD Application, Out of sync, Healthy) and `platform` (Flux Kustomization, Ready), with sources and revisions.
- [ ] **K2.** Do: `platform` → **Suspend** → apply. Expect: the diff `+ suspend: true`, then a Suspended badge and a **Resume** button. Press **Resume** and apply.
- [ ] **K3.** Do: `shop` → **Repository**. Expect: your browser opens `https://github.com/example/shop-deploy` (a sample address that doesn't exist).
- [ ] **K4.** (Optional, needs a GitHub account.) A pull request instead of an edit:
  1. On GitHub, create a **private** repository, for example `kupi-pr-test`, and add a file `apps/ticker.yaml` holding the ticker Deployment (copy it from `docs\testdata\demo-workloads.yaml`, the part between the `---` lines that has `name: ticker`).
  2. Point the sample Argo CD app at it, with Kupi: sidebar → Custom resources → **Applications** → `shop` → YAML → **Edit with form or YAML**. In the YAML, under `source:`, set `repoURL: https://github.com/<you>/kupi-pr-test` (your GitHub user name in place of `<you>`) and `path: apps`, keep `targetRevision: main` → Preview change → apply.

  3. On GitHub: Settings → Developer settings → Fine-grained tokens → **Generate new token**, only for `kupi-pr-test`, with **Contents: Read and write** and **Pull requests: Read and write**.
  4. In Kupi: unlock the vault (H5), GitOps → **GitHub** box → paste the token → **Check and save**. Expect: "Connected as <you>".
  5. Open the `ticker` Deployment → YAML → **Edit with form or YAML**. Expect: a yellow notice that Argo CD manages it, with **Propose in Git**. Change Copies running to 3 → **Propose in Git**. Expect: "Open a pull request?" with your repository, "into main", the file `apps/ticker.yaml` and a diff with `replicas: 2 → 3`. Press **Open pull request**. Expect: a link to the new pull request; on GitHub it changes only that line, from a branch named `kupi/deployment-ticker-…`.
  6. Close the pull request on GitHub, delete the token there, and in Kupi press **Forget token**.

### L. Images

- [ ] **L1.** Do: sidebar → Images. Expect: your local Docker images with size and age.
- [ ] **L2.** Do: search Docker Hub for `busybox`, press **Versions** to see its tags, then pull `busybox:1.36` (type it in the image box and press **Pull**). Expect: progress, then the image in the local list.
- [ ] **L3.** Do: on `busybox:1.36` → **Load into cluster** → `k3d-kupi-test`. Expect: a success message once it's on the node.
- [ ] **L4.** Do: **Details** on an image. Expect: layers, ports, user and environment variable names only.
- [ ] **L5.** (Optional, needs an account on a registry with a private image, for example Docker Hub or ghcr.io.) Do: unlock the vault, **Registry logins** tab → registry (`docker.io` or `ghcr.io`), user name, an access token → **Save login**. Then pull your private image. Expect: "Pulled …"; without the login it fails with "unauthorized" or "no basic auth credentials". The password is never shown again; the bin icon removes the login. With the vault locked (**Lock now** on the Vault page), the failed pull adds that the vault is locked and no saved login was used.
- [ ] **L6.** (Needs Trivy.) Do: **Details** on `busybox:1.36` → **Scan for vulnerabilities**. Expect: after a while (the first scan downloads Trivy's database), "No known vulnerabilities" or a table sorted worst first, with package, installed and fixed versions. Without Trivy, the button is disabled and a note says where to get it.
- [ ] **L7.** (Needs cosign.) Do: pull `cgr.dev/chainguard/static:latest`, open its **Details** → **Check signature**. Expect: **Signed**, with the signer (a `github.com/chainguard-images/…` workflow) and the issuer `https://token.actions.githubusercontent.com`. Then **Check signature** on `busybox:1.36`. Expect: "No signature was found for this image."

### M. Local clusters and laptop mode

- [ ] **M1.** Do: sidebar → Local clusters. Expect: `kupi-test`, Running.
- [ ] **M2.** Do: **Laptop mode**. Expect: memory and CPU use with bars, and the node's own numbers.
- [ ] **M3.** Do: memory cap `2048`, CPU cap `1.5` → **Apply caps**. Expect: "Caps applied". Clear both fields and apply again to lift them.
- [ ] **M4.** Do: **Stop when idle** → After 15 minutes. Disconnect the cluster on the Clusters page and leave Kupi alone for 16 minutes, then reload the Local clusters page. Expect: `kupi-test` shows Stopped, and the audit log has "local-cluster-idle-stop".
- [ ] **M5.** Do: Clusters → **Connect** `k3d-kupi-test`. Expect: it takes longer (the cluster starts), then connects. Set Stop when idle back to **Never**.
- [ ] **M6.** Do: **New cluster**, name `scratch`, 0 agents → create; then delete it (type its name). Expect: it appears Running, then disappears. Your current kubectl context didn't change (`kubectl config current-context` shows the same as before).

### N. Settings

- [ ] **N1.** Do: **Settings** (bottom of the sidebar). Expect: sections Appearance, Logs, Map, Security and privacy, Terminal, Clusters and credentials, About (with the logo and the settings folder).
- [ ] **N2.** Do: Theme → **Light**, then **Dark**, then **System**. Expect: the whole window switches at once.
- [ ] **N3.** Do: Interface size → **125%**, then back to **100%**. Expect: everything gets bigger, then normal.
- [ ] **N4.** Do: Clear copied secrets after → **10 seconds**. Copy a Secret value (G3), paste in Notepad after 15 seconds. Expect: nothing is pasted. Set it back to 30 seconds.
- [ ] **N5.** Do: Map → Refresh every → **5 seconds**, open the Map. Expect: the header says "refreshes every 5 s".
- [ ] **N6.** Do: open the audit log. Expect: "settings-changed" entries for the clipboard change.

### O. Terminal

- [ ] **O1.** Do: sidebar → **Terminal** (or press **Ctrl+`**). Expect: an explanation, a warning that commands there skip Kupi's preview and audit log, and **Turn on the terminal…**.
- [ ] **O2.** Do: **Turn on the terminal…**. Expect: a **Windows dialog** (not a Kupi one) asking to turn it on, with **Cancel** as the default. Press Cancel. Expect: "The terminal stays off". Try again and press **Turn on the terminal**. Expect: a PowerShell prompt appears.
- [ ] **O3.** Do: look under the tabs. Expect: "kubectl and helm here use k3d-kupi-test · namespace kupi-demo" (or the namespace you chose) and a **Local** badge.
- [ ] **O4.** Do: type `kubectl get pods` and Enter. Expect: the pods of that namespace, without typing `-n`. Then `kubectl config current-context`. Expect: `k3d-kupi-test`.
- [ ] **O5.** Do: type `kubectl config set-context --current --namespace=kube-system`, then `kubectl get pods`. Expect: the kube-system pods. Then open a normal PowerShell window (outside Kupi) and run `kubectl config view --minify`. Expect: no `namespace: kube-system`; your kubeconfig wasn't changed.
- [ ] **O6.** Do: copy these two lines in Notepad, then paste in the terminal with Ctrl+V:

  ```
  kubectl get ns
  kubectl get nodes
  ```

  Expect: a "Paste and run?" dialog showing both lines. Press **Paste as one line, don't run**. Expect: `kubectl get ns ; kubectl get nodes` waits at the prompt; press Enter to run it.
- [ ] **O7.** Do: the lightning icon (**Snippets**) → "Recent events, newest last". Expect: the command is typed at the prompt but doesn't run until you press Enter.
- [ ] **O8.** Do: the wrench icon (**Tools**). Expect: kubectl, helm, docker and k3d with their versions, the cluster version, and no version warning.
- [ ] **O9.** Do: **Ctrl+Shift+F**, search `Running`. Expect: matches highlighted; Enter jumps to the next one.
- [ ] **O10.** Do: **+** (or Ctrl+Shift+T) for a second tab, then go to another page (Clusters) and come back with Ctrl+`. Expect: both tabs are still there with their output.
- [ ] **O11.** Do: type `exit` in a tab. Expect: "[The shell ended… Press Enter to start a new one.]"; Enter starts a new shell in the same tab.
- [ ] **O12.** Do: change the cluster's profile to **Production** (C2), reconnect, open a new terminal tab. Expect: a red bar saying commands skip Kupi's preview and audit log. Paste `kubectl delete pod crasher -n kupi-demo` (one line). Expect: the paste dialog shows it in red as destructive. Press **Cancel**. Set the profile back to Local.
- [ ] **O13.** Do: Settings → Terminal → **Turn off**. Expect: the terminal tabs close; the Terminal page shows the explanation again.
- [ ] **O14.** Do: Settings → Snippets → **Add**: name `My pods`, command `kubectl get pods -o wide` → Save. Turn the terminal on again and open Snippets. Expect: "My snippets" lists it first.

### P. Audit log and closing

- [ ] **P1.** Do: **Audit** (bottom of the sidebar). Expect: your scales, edits, the four objects of D9 as separate entries, Secret reveals and copies, shell, Helm install, upgrade and rollback, GitOps suspend, laptop caps, terminal-enabled, terminal-opened and terminal-disabled, newest first (and, if you did K4, github-token-saved, gitops-pr-opened and github-token-deleted).
- [ ] **P2.** Do: **Verify**. Expect: the chain is reported intact.
- [ ] **P3.** Do: copy a Secret value (G3), then close Kupi within 30 seconds and paste in Notepad. Expect: nothing is pasted; Kupi cleared it on exit.
- [ ] **P4.** Do: look at Kupi in the taskbar and in File Explorer (`kupi.exe`). Expect: the new icon, a navy tile with a white K and a cyan arrow.

---

## Part 5. When something goes wrong

For each problem, write down:

1. The step number (for example **G5**).
2. What you did and what you expected.
3. What happened instead: the exact message, and a screenshot (Win+Shift+S).
4. The Kupi version shown at the bottom of the sidebar.

Useful files are in the test settings folder (`%TEMP%\kupi-test-config`): `audit.log`, and the `history` folder. They never contain Secret values, but they do contain names from your cluster.

---

## Part 6. Clean up

```powershell
k3d cluster delete kupi-test
```

```powershell
Remove-Item -Recurse -Force "$env:TEMP\kupi-test-config"
```

If you created a vault in H1, also delete the `kupi` entry in Windows Credential Manager (Kupi's **Delete the vault…** does this too).
