import { useEffect, useState } from "react";
import { CommandPalette } from "./components/CommandPalette";
import { ContextBar } from "./components/ContextBar";
import { Sidebar } from "./components/Sidebar";
import { AppService } from "./lib/api";
import { ApplyPage } from "./pages/ApplyPage";
import { AuditPage } from "./pages/AuditPage";
import { ClustersPage } from "./pages/ClustersPage";
import { ResourcesPage } from "./pages/ResourcesPage";
import { ClustersProvider } from "./state/clusters";
import { NavProvider, useNav } from "./state/nav";

function Main() {
  const { route, navigate } = useNav();
  const openPods = () => navigate({ page: "resources", group: "", resource: "pods" });
  return (
    <main className={`min-h-0 flex-1 px-6 py-5 ${route.page === "resources" ? "overflow-hidden" : "overflow-y-auto"}`}>
      {route.page === "clusters" && <ClustersPage onOpened={openPods} />}
      {route.page === "apply" && <ApplyPage key={route.manifest ?? ""} initialManifest={route.manifest} onConnect={() => navigate({ page: "clusters" })} />}
      {route.page === "audit" && <AuditPage />}
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
