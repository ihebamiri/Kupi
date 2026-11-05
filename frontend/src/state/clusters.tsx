import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { ClusterService, ResourceService, errorMessage, type ContextInfo } from "../lib/api";

interface ClustersState {
  contexts: ContextInfo[];
  loading: boolean;
  error: string | null;
  /** The connected context the rest of the app works on. */
  active: ContextInfo | null;
  setActive: (name: string) => void;
  /** Namespace in view; "" means all namespaces. */
  namespace: string;
  setNamespace: (ns: string) => void;
  namespaces: string[];
  refresh: () => Promise<void>;
  connect: (name: string) => Promise<void>;
  disconnect: (name: string) => Promise<void>;
  trust: (file: string) => Promise<void>;
  setProfile: (name: string, kind: string) => Promise<void>;
  setEditing: (name: string, enabled: boolean, reason: string) => Promise<void>;
}

const ClustersContext = createContext<ClustersState | null>(null);

export function ClustersProvider({ children }: { children: ReactNode }) {
  const [contexts, setContexts] = useState<ContextInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeName, setActiveName] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const list = await ClusterService.ListContexts();
      setContexts(list ?? []);
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const connect = useCallback(
    async (name: string) => {
      await ClusterService.Connect(name);
      setActiveName(name);
      await refresh();
    },
    [refresh],
  );

  const disconnect = useCallback(
    async (name: string) => {
      await ClusterService.Disconnect(name);
      setActiveName((current) => (current === name ? null : current));
      await refresh();
    },
    [refresh],
  );

  const trust = useCallback(
    async (file: string) => {
      await ClusterService.TrustKubeconfig(file);
      await refresh();
    },
    [refresh],
  );

  const setProfile = useCallback(
    async (name: string, kind: string) => {
      await ClusterService.SetProfile(name, kind);
      setActiveName((current) => (current === name ? null : current));
      await refresh();
    },
    [refresh],
  );

  const setEditing = useCallback(
    async (name: string, enabled: boolean, reason: string) => {
      await ClusterService.SetEditing(name, enabled, reason);
      await refresh();
    },
    [refresh],
  );

  const active = useMemo(
    () => contexts.find((c) => c.name === activeName && c.connected) ?? null,
    [contexts, activeName],
  );

  // Namespace in view. Production starts scoped to one namespace to keep load
  // on the API server low; other clusters start with all namespaces.
  const [namespace, setNamespace] = useState("");
  const [namespaces, setNamespaces] = useState<string[]>([]);
  const activeKey = active ? `${active.name}|${active.profile}` : "";
  useEffect(() => {
    if (!active) {
      setNamespaces([]);
      return;
    }
    setNamespace(active.profile === "production" ? active.namespace || "default" : "");
    let cancelled = false;
    ResourceService.Namespaces(active.name)
      .then((list) => !cancelled && setNamespaces(list ?? []))
      .catch(() => !cancelled && setNamespaces([]));
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeKey]);

  const value: ClustersState = {
    contexts,
    loading,
    error,
    active,
    setActive: setActiveName,
    namespace,
    setNamespace,
    namespaces,
    refresh,
    connect,
    disconnect,
    trust,
    setProfile,
    setEditing,
  };
  return <ClustersContext.Provider value={value}>{children}</ClustersContext.Provider>;
}

export function useClusters(): ClustersState {
  const state = useContext(ClustersContext);
  if (!state) throw new Error("useClusters must be used inside ClustersProvider");
  return state;
}
