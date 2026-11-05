import { useEffect, useState } from "react";
import { ContextBar } from "./components/ContextBar";
import { Sidebar, type Page } from "./components/Sidebar";
import { AppService } from "./lib/api";
import { ApplyPage } from "./pages/ApplyPage";
import { AuditPage } from "./pages/AuditPage";
import { ClustersPage } from "./pages/ClustersPage";
import { ClustersProvider } from "./state/clusters";

export default function App() {
  const [page, setPage] = useState<Page>("clusters");
  const [version, setVersion] = useState("");

  useEffect(() => {
    AppService.Info()
      .then((info) => setVersion(info.version))
      .catch(() => setVersion(""));
  }, []);

  return (
    <ClustersProvider>
      <div className="flex h-full">
        <Sidebar page={page} onNavigate={setPage} version={version} />
        <div className="flex min-w-0 flex-1 flex-col">
          <ContextBar />
          <main className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
            {page === "clusters" && <ClustersPage onOpened={() => setPage("apply")} />}
            {page === "apply" && <ApplyPage onConnect={() => setPage("clusters")} />}
            {page === "audit" && <AuditPage />}
          </main>
        </div>
      </div>
    </ClustersProvider>
  );
}
