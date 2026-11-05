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

- [ ] **A1.** Do: look at the window. Expect: a dark window titled "Kupi" with the sidebar on the left and the Clusters page open.
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

### E. Overview and Map

- [ ] **E1.** Do: sidebar → Overview. Expect: the node with CPU and memory bars, and the busiest pods.
- [ ] **E2.** Do: sidebar → Map (namespace `kupi-demo`). Expect: boxes for `ticker` (2/2), `demo-web`, `hello` and the failing `crasher` pod; `demo-web` is linked to the ConfigMap `demo-web-config` and the Secret `demo-web-auth`.
- [ ] **E3.** Do: switch the overlay to **Security**. Expect: `crasher` flagged for no memory limit and possibly running as root; `ticker` flagged as possibly running as root; `demo-web` and `hello` unflagged (they run as non-root with limits).
- [ ] **E4.** Do: click `demo-web-config`. Expect: it and `demo-web` are highlighted, and the panel says a change affects `demo-web`.

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

### K. GitOps

- [ ] **K1.** Do: sidebar → GitOps. Expect: `shop` (Argo CD Application, Out of sync, Healthy) and `platform` (Flux Kustomization, Ready), with sources and revisions.
- [ ] **K2.** Do: `platform` → **Suspend** → apply. Expect: the diff `+ suspend: true`, then a Suspended badge and a **Resume** button. Press **Resume** and apply.
- [ ] **K3.** Do: `shop` → **Repository**. Expect: your browser opens `https://github.com/example/shop-deploy` (a sample address that doesn't exist).

### L. Images

- [ ] **L1.** Do: sidebar → Images. Expect: your local Docker images with size and age.
- [ ] **L2.** Do: search Docker Hub for `busybox`, press **Versions** to see its tags, then pull `busybox:1.36` (type it in the image box and press **Pull**). Expect: progress, then the image in the local list.
- [ ] **L3.** Do: on `busybox:1.36` → **Load into cluster** → `k3d-kupi-test`. Expect: a success message once it's on the node.
- [ ] **L4.** Do: **Details** on an image. Expect: layers, ports, user and environment variable names only.

### M. Local clusters and laptop mode

- [ ] **M1.** Do: sidebar → Local clusters. Expect: `kupi-test`, Running.
- [ ] **M2.** Do: **Laptop mode**. Expect: memory and CPU use with bars, and the node's own numbers.
- [ ] **M3.** Do: memory cap `2048`, CPU cap `1.5` → **Apply caps**. Expect: "Caps applied". Clear both fields and apply again to lift them.
- [ ] **M4.** Do: **Stop when idle** → After 15 minutes. Disconnect the cluster on the Clusters page and leave Kupi alone for 16 minutes, then reload the Local clusters page. Expect: `kupi-test` shows Stopped, and the audit log has "local-cluster-idle-stop".
- [ ] **M5.** Do: Clusters → **Connect** `k3d-kupi-test`. Expect: it takes longer (the cluster starts), then connects. Set Stop when idle back to **Never**.
- [ ] **M6.** Do: **New cluster**, name `scratch`, 0 agents → create; then delete it (type its name). Expect: it appears Running, then disappears. Your current kubectl context didn't change (`kubectl config current-context` shows the same as before).

### N. Audit log and closing

- [ ] **N1.** Do: sidebar → Audit log. Expect: your scales, edits, Secret reveals and copies, shell, Helm upgrade and rollback, GitOps suspend, laptop caps and so on, newest first.
- [ ] **N2.** Do: **Verify**. Expect: the chain is reported intact.
- [ ] **N3.** Do: copy a Secret value (G3), then close Kupi within 30 seconds and paste in Notepad. Expect: nothing is pasted; Kupi cleared it on exit.

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
