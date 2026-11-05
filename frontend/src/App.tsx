import { Suspense, lazy, useEffect, useState } from "react";
import { CommandPalette } from "./components/CommandPalette";
import { ContextBar } from "./components/ContextBar";
import { Sidebar } from "./components/Sidebar";
import { AppService } from "./lib/api";
// The map pulls in React Flow and the ELK layout engine; load it only when opened.
const CanvasPage = lazy(() => import("./pages/CanvasPage").then((m) => ({ default: m.CanvasPage })));
// The editor pulls in CodeMirror; load it only when opened.
const EditorPage = lazy(() => import("./pages/EditorPage").then((m) => ({ default: m.EditorPage })));
import { AuditPage } from "./pages/AuditPage";
import { ClustersPage } from "./pages/ClustersPage";
import { ForwardsPage } from "./pages/ForwardsPage";
import { ImagesPage } from "./pages/ImagesPage";
import { LocalClustersPage } from "./pages/LocalClustersPage";
import { OverviewPage } from "./pages/OverviewPage";
import { ResourcesPage } from "./pages/ResourcesPage";
import { ClustersProvider } from "./state/clusters";
import { NavProvider, useNav } from "./state/nav";

function Main() {
  const { route, navigate } = useNav();
  const openPods = () => navigate({ page: "overview" });
  return (
    <main className={`min-h-0 flex-1 px-6 py-5 ${route.page === "resources" || route.page === "apply" || route.page === "canvas" ? "overflow-hidden" : "overflow-y-auto"}`}>
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
      {/* Keyed by type so selection and sort never carry over to another type. */}
      {route.page === "resources" && <ResourcesPage key={`${route.group}/${route.resource}`} group={route.group} resource={route.resource} />}
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
  );
}
