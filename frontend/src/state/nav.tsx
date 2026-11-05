import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { ResourceService, errorMessage, type ResourceRef, type ResourceTypeView } from "../lib/api";
import { useClusters } from "./clusters";

export type Route =
  | { page: "clusters" }
  | { page: "apply"; manifest?: string; edit?: ResourceRef }
  | { page: "audit" }
  | { page: "overview" }
  | { page: "canvas" }
  | { page: "forwards" }
  | { page: "resources"; group: string; resource: string };

interface NavState {
  route: Route;
  navigate: (route: Route) => void;
  /** Resource types of the active cluster, in sidebar order. */
  types: ResourceTypeView[];
  typesError: string | null;
  findType: (group: string, resource: string) => ResourceTypeView | undefined;
  reloadTypes: () => void;
}

const NavContext = createContext<NavState | null>(null);

export function NavProvider({ children }: { children: ReactNode }) {
  const { active } = useClusters();
  const [route, setRoute] = useState<Route>({ page: "clusters" });
  const [types, setTypes] = useState<ResourceTypeView[]>([]);
  const [typesError, setTypesError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const activeName = active?.name ?? "";

  useEffect(() => {
    setTypes([]);
    setTypesError(null);
    if (!activeName) return;
    let cancelled = false;
    ResourceService.Types(activeName)
      .then((list) => !cancelled && setTypes(list ?? []))
      .catch((err) => !cancelled && setTypesError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [activeName, reload]);

  // Leave resource pages when the cluster goes away.
  useEffect(() => {
    if (!activeName && (route.page === "resources" || route.page === "overview" || route.page === "canvas")) setRoute({ page: "clusters" });
  }, [activeName, route.page]);

  const findType = useCallback(
    (group: string, resource: string) => types.find((t) => t.group === group && t.resource === resource),
    [types],
  );

  const value = useMemo<NavState>(
    () => ({ route, navigate: setRoute, types, typesError, findType, reloadTypes: () => setReload((n) => n + 1) }),
    [route, types, typesError, findType],
  );
  return <NavContext.Provider value={value}>{children}</NavContext.Provider>;
}

export function useNav(): NavState {
  const state = useContext(NavContext);
  if (!state) throw new Error("useNav must be used inside NavProvider");
  return state;
}
