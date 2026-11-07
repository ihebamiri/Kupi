# Testing Kupi on Linux

Kupi behaves the same on Linux and Windows, so the feature tests are the ones in [Testing on Windows](TESTING-WINDOWS.md). This guide covers what's different: which Linux versions work, how to install the packages, the commands for the test cluster, and the checks that only make sense on Linux.

Time needed: about 20 minutes for setup, then the Windows guide's Part 4.

---

## Part 1. Which Linux

Kupi's window uses **GTK 4.10 or newer** and **WebKitGTK 6.0**. That means:

| Distribution | Works? |
|---|---|
| Ubuntu 24.04 LTS and newer | yes (tested on 24.04) |
| Debian 13 (trixie) and newer | should work, not tested |
| Fedora 40 and newer | should work, not tested |
| Ubuntu 22.04, Debian 12 and older | **no**: their GTK is too old (4.6 and 4.8) |

Only x86-64 (amd64) builds are published. Both Wayland and X11 desktops work. WSL2 on Windows 11 works too: Ubuntu 24.04 in WSL shows Kupi's window on the Windows desktop through WSLg.

---

## Part 2. Install Kupi

Download one of these from the [releases page](https://github.com/ihebamiri/Kupi/releases), and `SHA256SUMS` to check it:

```bash
sha256sum --ignore-missing -c SHA256SUMS
```

| File | For | Install |
|---|---|---|
| `kupi-<version>-linux-amd64.deb` | Ubuntu, Debian | `sudo apt install ./kupi-<version>-linux-amd64.deb` (pulls in GTK 4 and WebKitGTK) |
| `kupi-<version>-linux-x86_64.rpm` | Fedora, openSUSE | `sudo dnf install ./kupi-<version>-linux-x86_64.rpm` |
| `kupi-<version>-linux-x86_64.AppImage` | any distribution with the libraries | `chmod +x` it and run it. It needs FUSE 2 (`sudo apt install libfuse2t64` on Ubuntu 24.04), or set `APPIMAGE_EXTRACT_AND_RUN=1` |
| `kupi-<version>-linux-amd64.tar.gz` | anything else | unpack and run `./kupi`; install `libgtk-4-1` and `libwebkitgtk-6.0-4` (Ubuntu names) yourself |

The .deb and .rpm put `kupi` in `/usr/bin`, a **Kupi** entry in the applications menu, and the license in `/usr/share/doc/kupi`. Remove them with `sudo apt remove kupi` or `sudo dnf remove kupi`.

To build from source instead, see the README's "Build from source": you need `libgtk-4-dev` and `libwebkitgtk-6.0-dev`, then `wails3 task linux:build` puts the binary in `bin/kupi`.

---

## Part 3. Tools for the tests

As on Windows, Kupi itself needs none of these; the tests use them to make a local cluster and load sample data.

1. **Docker Engine.** On Ubuntu:

   ```bash
   sudo apt install docker.io
   ```

   ```bash
   sudo usermod -aG docker $USER
   ```

   Log out and back in (in WSL: `wsl --shutdown` from Windows, then reopen Ubuntu), then check that `docker version` shows a server without `sudo`. Docker Desktop for Linux, or Docker Desktop on Windows with WSL integration switched on for this distribution, also works. Podman works for the Images page, not for k3d.

2. **k3d, kubectl and Helm.** Follow each project's install page: [k3d.io](https://k3d.io), [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/) and [Helm](https://helm.sh/docs/intro/install/). On Ubuntu with snap: `sudo snap install kubectl --classic` and `sudo snap install helm --classic`.

3. **Optional:** [Trivy](https://trivy.dev) and [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) for the image security steps (L6, L7).

4. Get the test data: clone the repository (or download its zip) and work from its folder.

   ```bash
   git clone https://github.com/ihebamiri/Kupi.git && cd Kupi
   ```

5. Use a separate settings folder for the test, so it leaves your real settings, audit log and vault alone. This lasts for this terminal window; start Kupi from it.

   ```bash
   export KUPI_CONFIG_DIR=/tmp/kupi-test-config
   ```

Then follow **Part 3 of the Windows guide** (create the cluster and the test data). The commands are the same with `/` in paths, for example:

```bash
kubectl --context k3d-kupi-test apply -f docs/testdata/demo-workloads.yaml
```

---

## Part 4. Run the tests

Start Kupi from the same terminal:

```bash
kupi
```

(or `./kupi` for the tar.gz, or the AppImage's path). Then work through **Part 4 of the Windows guide**, with these differences:

| Step | On Linux |
|---|---|
| A1 | The window follows your desktop's light or dark setting and has your desktop's title bar. |
| G3, N4, P3 | Paste into any text editor. Kupi clears the clipboard after the timeout, as on Windows. |
| G4 | Skip it. Linux has no standard way to keep an entry out of clipboard history: clipboard managers (GNOME extensions, KDE's Klipper, CopyQ…) may keep copied values. Turn their history off while handling secrets. |
| H1 | The first choice reads "In the Secret Service (GNOME Keyring or KeePassXC)" or "In KWallet". If it says "No password store was found on this system" (common in WSL and minimal desktops), choose **Behind a passphrase**. |
| H5 | With a password store: unlocks without typing. With a passphrase: asks for it. |
| H6 | GNOME: open **Passwords and Keys** (Seahorse) → Login. KDE: **KWalletManager**. Expect an entry named `kupi`. |
| O2 | The confirmation is a GTK dialog from your desktop, not a Windows one. |
| O2–O14 | The shell is your login shell (`$SHELL`, usually bash) first, then the other shells in `/etc/shells` (zsh, fish, sh) as extra choices. Use another terminal app for O5's "outside Kupi" check. |
| O6 | Copy the two lines from a text editor; paste with **Ctrl+Shift+V** or Ctrl+V. |
| P4 | Look for **Kupi** in the applications menu (.deb and .rpm), with the navy logo. |

---

## Part 5. Linux-only checks

- [ ] **X1.** Do: open the applications menu and start **Kupi** from there (.deb or .rpm). Expect: the same window as from the terminal. It uses your real settings folder (`~/.config/kupi`), because the menu doesn't see `KUPI_CONFIG_DIR`.
- [ ] **X2.** Do: in the test settings folder, run `ls -la /tmp/kupi-test-config`. Expect: the folder is `drwx------` and every file `-rw-------` (only you can read them).
- [ ] **X3.** Do: with the terminal on (O2), run `echo $TERM; tput cols`, then make the window narrower and run `tput cols` again. Expect: `xterm-256color` and a smaller number the second time.
- [ ] **X4.** Do: in the terminal, run `printf '\e[31mred\e[0m ✓ 日本\n'`. Expect: "red" in red, a check mark and the Japanese characters, aligned.
- [ ] **X5.** Do: Settings → Appearance → Interface size **150%**. Expect: everything scales sharply (no blur), as on a high-resolution screen. Set it back.
- [ ] **X6.** Do: close Kupi with the window's close button while a terminal tab and a port-forward are open. Expect: Kupi exits within a few seconds, and `pgrep -a kupi` prints nothing afterwards.

---

## Part 6. When something goes wrong

| Problem | Try |
|---|---|
| `error while loading shared libraries: libwebkitgtk-6.0.so.4` | Install `libwebkitgtk-6.0-4` and `libgtk-4-1`, or use the .deb, which pulls them in. On Ubuntu 22.04 they don't exist: upgrade to 24.04. |
| The window opens but stays white or black | A graphics driver problem in WebKitGTK, common with NVIDIA. Start Kupi with `WEBKIT_DISABLE_DMABUF_RENDERER=1 kupi`. |
| The AppImage says "dlopen(): error loading libfuse.so.2" | `sudo apt install libfuse2t64`, or run it with `APPIMAGE_EXTRACT_AND_RUN=1`. |
| The Images page says Docker isn't running | Check `docker version` works without `sudo` (step 1 of Part 3). Kupi looks for `$DOCKER_HOST` (a `unix://` address), `/var/run/docker.sock`, Docker Desktop's socket and Podman's socket. |
| Vault: "No password store was found" on a full desktop | The Secret Service isn't running. On GNOME, install `gnome-keyring` and log in again. Or use a passphrase. |
| In WSL, no window appears | Check WSLg works: `echo $WAYLAND_DISPLAY` should print `wayland-0`. Update WSL from Windows with `wsl --update`. |

When reporting a problem, include the step, your distribution and version (`cat /etc/os-release`), your desktop (GNOME, KDE…, Wayland or X11), and what Kupi printed in the terminal.

---

## Part 7. Clean up

```bash
k3d cluster delete kupi-test
```

```bash
rm -rf /tmp/kupi-test-config
```

If you created a vault with the password store in H1, Kupi's **Delete the vault…** also removes its `kupi` entry.
