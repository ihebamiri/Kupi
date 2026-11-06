import { Suspense, lazy, useEffect, useState } from "react";
import { CommandPalette } from "./components/CommandPalette";
import { ContextBar } from "./components/ContextBar";
import { Sidebar } from "./components/Sidebar";
import { AppService } from "./lib/api";
// The map pulls in React Flow and the ELK layout engine; load it only when opened.
const CanvasPage = lazy(() => import("./pages/CanvasPage").then((m) => ({ default: m.CanvasPage })));
// The editor pulls in CodeMirror; load it only when opened.
const EditorPage = lazy(() => import("./pages/EditorPage").then((m) => ({ default: m.EditorPage })));
// The Helm page uses the YAML editor too; load it only when opened.
const HelmPage = lazy(() => import("./pages/HelmPage").then((m) => ({ default: m.HelmPage })));
import { AuditPage } from "./pages/AuditPage";
import { ClustersPage } from "./pages/ClustersPage";
import { ForwardsPage } from "./pages/ForwardsPage";
import { ImagesPage } from "./pages/ImagesPage";
import { LocalClustersPage } from "./pages/LocalClustersPage";
import { OverviewPage } from "./pages/OverviewPage";
import { GitOpsPage } from "./pages/GitOpsPage";
import { HistoryPage } from "./pages/HistoryPage";
import { IncidentPage } from "./pages/IncidentPage";
import { ResourcesPage } from "./pages/ResourcesPage";
import { SecretsPage } from "./pages/SecretsPage";
import { VaultPage } from "./pages/VaultPage";
import { SettingsPage } from "./pages/SettingsPage";
import { ClustersProvider } from "./state/clusters";
import { NavProvider, useNav } from "./state/nav";
import { SettingsProvider } from "./state/settings";
// xterm.js and its add-ons load with the terminal.
const TerminalPage = lazy(() => import("./pages/TerminalPage").then((m) => ({ default: m.TerminalPage })));

const FULL_HEIGHT = new Set(["resources", "apply", "canvas", "secrets", "helm", "terminal"]);

function Main() {
  const { route, navigate } = useNav();
  const openPods = () => navigate({ page: "overview" });
  // The terminal stays mounted once opened, so its shells and scrollback
  // survive a visit to another page.
  const [terminalOpened, setTerminalOpened] = useState(false);
  useEffect(() => {
    if (route.page === "terminal") setTerminalOpened(true);
  }, [route.page]);

  // Ctrl+` opens the terminal (and goes back where you were on a second press).
  const [previous, setPrevious] = useState<typeof route | null>(null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey && !e.shiftKey && !e.altKey && e.code === "Backquote") {
        e.preventDefault();
        if (route.page === "terminal") navigate(previous ?? { page: "clusters" });
        else {
          setPrevious(route);
          navigate({ page: "terminal" });
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [route, previous, navigate]);

  return (
    <main className={`min-h-0 flex-1 px-6 py-5 ${FULL_HEIGHT.has(route.page) ? "overflow-hidden" : "overflow-y-auto"}`}>
      {terminalOpened && (
        <div className={route.page === "terminal" ? "h-full" : "hidden"}>
          <Suspense fallback={<p className="text-[13px] text-muted">Loading the terminal…</p>}>
            <TerminalPage visible={route.page === "terminal"} />
          </Suspense>
        </div>
      )}
      {route.page === "settings" && <SettingsPage />}
      {route.page === "clusters" && <ClustersPage onOpened={openPods} />}
      {route.page === "apply" && (
        <Suspense fallback={<p className="text-[13px] text-muted">Loading the editor…</p>}>
          <EditorPage
            key={route.edit ? `${route.edit.group}/${route.edit.resource}/${route.edit.namespace}/${route.edit.name}` : (route.fromImage ?? route.manifest ?? "new")}
            initialManifest={route.manifest}
            edit={route.edit}
            fromImage={route.fromImage}
            onConnect={() => navigate({ page: "clusters" })}
          />
        </Suspense>
      )}
      {route.page === "audit" && <AuditPage />}
      {route.page === "overview" && <OverviewPage />}
      {route.page === "canvas" && (
        <Suspense fallback={<p className="text-[13px] text-muted">Loading the map…</p>}>
          <CanvasPage />
        </Suspense>
      )}
      {route.page === "forwards" && <ForwardsPage />}
      {route.page === "images" && <ImagesPage />}
      {route.page === "local" && <LocalClustersPage />}
      {route.page === "secrets" && <SecretsPage key={`${route.namespace ?? ""}/${route.name ?? ""}`} initial={route.name ? { namespace: route.namespace ?? "", name: route.name } : undefined} />}
      {route.page === "vault" && <VaultPage />}
      {route.page === "history" && <HistoryPage />}
      {route.page === "incident" && <IncidentPage />}
      {route.page === "gitops" && <GitOpsPage />}
      {route.page === "helm" && (
        <Suspense fallback={<p className="text-[13px] text-muted">Loading…</p>}>
          <HelmPage />
        </Suspense>
      )}
      {/* Keyed by type so selection and sort never carry over to another type. */}
      {route.page === "resources" && <ResourcesPage key={`${route.group}/${route.resource}/${route.select?.namespace ?? ""}/${route.select?.name ?? ""}`} group={route.group} resource={route.resource} select={route.select} />}
    </main>
  );
}

export default function App() {
  const [version, setVersion] = useState("");

  useEffect(() => {
    AppService.Info()
      .then((info) => setVersion(info.version))
      .catch(() => setVersion(""));
  }, []);

  return (
    <SettingsProvider>
      <ClustersProvider>
        <NavProvider>
          <div className="flex h-full">
            <Sidebar version={version} />
            <div className="flex min-w-0 flex-1 flex-col">
              <ContextBar />
              <Main />
            </div>
          </div>
          <CommandPalette />
        </NavProvider>
      </ClustersProvider>
    </SettingsProvider>
  );
}
