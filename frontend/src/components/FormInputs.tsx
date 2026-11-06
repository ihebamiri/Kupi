import { createContext, useContext, useEffect, useId, useMemo, useRef, useState } from "react";
import { ChevronDown, LoaderCircle } from "lucide-react";
import { FormService, errorMessage } from "../lib/api";
import { getIn, type Path } from "../lib/objpath";
import { CPU_SCALE, MEMORY_SCALE, STORAGE_SCALE, describeQuantity, nearestStop, scaleValue, type Scale } from "../lib/quantity";
import { CLUSTER_WIDE, type Ref } from "../lib/refs";
import { inputClass } from "./ui";

/** Where the form's suggestions come from. */
export interface FormEnv {
  contextName: string;
  /** Namespace to use when the object doesn't name one. */
  namespace: string;
}

export const FormEnvContext = createContext<FormEnv>({ contextName: "", namespace: "default" });

const TYPE_LABEL: Record<string, string> = {
  namespaces: "namespace",
  configmaps: "ConfigMap",
  secrets: "Secret",
  serviceaccounts: "ServiceAccount",
  services: "Service",
  persistentvolumeclaims: "PersistentVolumeClaim",
  persistentvolumes: "PersistentVolume",
  nodes: "node",
  deployments: "Deployment",
  statefulsets: "StatefulSet",
  replicasets: "ReplicaSet",
  storageclasses: "StorageClass",
  ingressclasses: "IngressClass",
  priorityclasses: "PriorityClass",
  roles: "Role",
  clusterroles: "ClusterRole",
};

// Suggestions are cached briefly so moving between fields stays instant.
const cache = new Map<string, { at: number; names: string[] }>();
const CACHE_MS = 20_000;

function useSuggestions(contextName: string, ref: Ref, namespace: string, enabled: boolean) {
  const key = ref.keysOf ? `${contextName}|keys|${ref.keysOf.type}|${namespace}|${ref.keysOf.name}` : `${contextName}|${ref.type}|${CLUSTER_WIDE.has(ref.type) ? "" : namespace}`;
  const [state, setState] = useState<{ key: string; names: string[]; loading: boolean; error: string | null }>({ key: "", names: [], loading: false, error: null });

  useEffect(() => {
    if (!enabled || !contextName) return;
    const hit = cache.get(key);
    if (hit && Date.now() - hit.at < CACHE_MS) {
      setState({ key, names: hit.names, loading: false, error: null });
      return;
    }
    let cancelled = false;
    setState((s) => ({ key, names: s.key === key ? s.names : [], loading: true, error: null }));
    const load = ref.keysOf ? FormService.Keys(contextName, ref.keysOf.type, namespace, ref.keysOf.name) : FormService.Suggest(contextName, ref.type, namespace);
    load
      .then((names) => {
        const list = names ?? [];
        cache.set(key, { at: Date.now(), names: list });
        if (!cancelled) setState({ key, names: list, loading: false, error: null });
      })
      .catch((err) => !cancelled && setState({ key, names: [], loading: false, error: errorMessage(err) }));
    return () => {
      cancelled = true;
    };
  }, [key, enabled, contextName, namespace, ref.type, ref.keysOf]);

  return state.key === key ? state : { key, names: [], loading: enabled, error: null };
}

interface RefInputProps {
  label: string;
  value: unknown;
  onChange: (v: string | undefined) => void;
  refSpec: Ref;
  /** The whole object, to find its namespace. */
  root: unknown;
}

/**
 * A text field that also suggests existing objects: type any name, or pick
 * one from the list of what's in the cluster.
 */
export function RefInput({ label, value, onChange, refSpec, root }: RefInputProps) {
  const env = useContext(FormEnvContext);
  const objNs = getIn(root as never, ["metadata", "namespace"]);
  const namespace = refSpec.namespace ?? (typeof objNs === "string" && objNs ? objNs : env.namespace || "default");
  const [open, setOpen] = useState(false);
  const [touched, setTouched] = useState(false);
  const [active, setActive] = useState(-1);
  const { names, loading, error } = useSuggestions(env.contextName, refSpec, namespace, open || touched);
  const listId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const text = value === undefined || value === null ? "" : String(value);

  const shown = useMemo(() => {
    const q = text.toLowerCase();
    if (!q || names.includes(text)) return names;
    return names.filter((n) => n.toLowerCase().includes(q));
  }, [names, text]);

  const what = refSpec.keysOf ? `key of ${TYPE_LABEL[refSpec.keysOf.type]} ${refSpec.keysOf.name}` : TYPE_LABEL[refSpec.type] ?? refSpec.type;
  const where = refSpec.keysOf || CLUSTER_WIDE.has(refSpec.type) ? "" : ` in ${namespace}`;
  const missing = touched && !loading && !error && text !== "" && names.length > 0 && !names.includes(text);

  const pick = (name: string) => {
    onChange(name);
    setOpen(false);
    setActive(-1);
  };

  return (
    <div className="flex flex-col gap-1">
      <div className="relative">
        <input
          ref={inputRef}
          role="combobox"
          aria-label={label}
          aria-expanded={open}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={active >= 0 ? `${listId}-${active}` : undefined}
          className={`${inputClass} w-full pr-8`}
          value={text}
          placeholder={`Type or pick a ${what}`}
          spellCheck={false}
          autoComplete="off"
          onFocus={() => setOpen(true)}
          onBlur={() => {
            setOpen(false);
            setTouched(true);
          }}
          onChange={(e) => {
            onChange(e.target.value === "" ? undefined : e.target.value);
            setOpen(true);
            setActive(-1);
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setOpen(true);
              setActive((a) => Math.min(shown.length - 1, a + 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setActive((a) => Math.max(-1, a - 1));
            } else if (e.key === "Enter" && open && active >= 0 && shown[active]) {
              e.preventDefault();
              pick(shown[active]);
            } else if (e.key === "Escape" && open) {
              e.stopPropagation();
              setOpen(false);
            }
          }}
        />
        <button
          type="button"
          tabIndex={-1}
          aria-label={`Show ${what} suggestions`}
          className="absolute inset-y-0 right-0 flex w-8 items-center justify-center text-muted hover:text-fg"
          onMouseDown={(e) => {
            e.preventDefault();
            if (open) setOpen(false);
            else inputRef.current?.focus();
          }}
        >
          {loading && open ? <LoaderCircle className="size-3.5 animate-spin" /> : <ChevronDown className="size-4" />}
        </button>
        {open && (
          <ul
            id={listId}
            role="listbox"
            aria-label={`Existing ${what}${where}`}
            className="absolute z-30 mt-1 max-h-60 w-full overflow-auto rounded-md border border-line bg-panel py-1 shadow-xl shadow-black/30"
          >
            {error && <li className="px-2.5 py-1.5 text-[12px] text-bad">Couldn't load suggestions: {error}</li>}
            {!error && !loading && shown.length === 0 && (
              <li className="px-2.5 py-1.5 text-[12px] text-muted">{names.length === 0 ? `No ${what} found${where}.` : "No match. Your text is used as typed."}</li>
            )}
            {shown.map((n, i) => (
              <li
                key={n}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={n === text}
                onMouseDown={(e) => {
                  e.preventDefault();
                  pick(n);
                }}
                onMouseEnter={() => setActive(i)}
                className={`cursor-pointer truncate px-2.5 py-1.5 font-mono text-[12px] ${i === active ? "bg-accent/15 text-fg" : n === text ? "text-accent" : "text-fg"}`}
              >
                {n}
              </li>
            ))}
          </ul>
        )}
      </div>
      {missing && (
        <span className="text-[12px] text-warn">
          No {what} named {text}
          {where} yet. That's fine if this manifest or a later step creates it.
        </span>
      )}
    </div>
  );
}

const SCALES: Record<"cpu" | "memory" | "storage", Scale> = { cpu: CPU_SCALE, memory: MEMORY_SCALE, storage: STORAGE_SCALE };
const PLACEHOLDER: Record<"cpu" | "memory" | "storage", string> = { cpu: "e.g. 250m or 1", memory: "e.g. 512Mi or 1Gi", storage: "e.g. 10Gi" };

/** A quantity with a slider for quick changes and a text box for exact values. */
export function QuantitySlider({ kind, label, value, onChange }: { kind: "cpu" | "memory" | "storage"; label: string; value: unknown; onChange: (v: string | undefined) => void }) {
  const scale = SCALES[kind];
  const text = value === undefined || value === null ? "" : String(value);
  const set = text !== "";
  const idx = set ? nearestStop(scale, text) : 0;
  const valid = !set || scaleValue(scale, text) !== null;
  return (
    <div className="flex items-center gap-3">
      <input
        type="range"
        aria-label={`${label} slider`}
        min={0}
        max={scale.stops.length - 1}
        step={1}
        value={idx}
        onChange={(e) => onChange(scale.stops[Number(e.target.value)])}
        className={`kupi-range min-w-0 flex-1 ${set ? "" : "opacity-50"}`}
      />
      <input
        aria-label={label}
        className={`${inputClass} w-32 font-mono ${valid ? "" : "border-bad"}`}
        value={text}
        placeholder={PLACEHOLDER[kind]}
        spellCheck={false}
        onChange={(e) => onChange(e.target.value.trim() === "" ? undefined : e.target.value.trim())}
      />
      <span className={`w-24 shrink-0 text-[12px] ${valid ? "text-muted" : "text-bad"}`}>{set ? (valid ? describeQuantity(scale, text) : "Not a quantity") : "Not set"}</span>
    </div>
  );
}

/** A small count (replicas) with a slider and an exact number. */
export function CountSlider({ label, value, onChange }: { label: string; value: unknown; onChange: (v: number | undefined) => void }) {
  const n = typeof value === "number" ? value : undefined;
  const max = Math.max(10, n ?? 0);
  return (
    <div className="flex items-center gap-3">
      <input
        type="range"
        aria-label={`${label} slider`}
        min={0}
        max={max}
        step={1}
        value={n ?? 0}
        onChange={(e) => onChange(Number(e.target.value))}
        className={`kupi-range min-w-0 flex-1 ${n === undefined ? "opacity-50" : ""}`}
      />
      <input
        aria-label={label}
        className={`${inputClass} w-20 text-right tabular-nums`}
        inputMode="numeric"
        value={n === undefined ? "" : String(n)}
        placeholder="(not set)"
        onChange={(e) => {
          const raw = e.target.value.trim();
          if (raw === "") onChange(undefined);
          else if (/^\d+$/.test(raw)) onChange(Math.min(100000, Number(raw)));
        }}
      />
    </div>
  );
}

/**
 * resources.requests or resources.limits: CPU and memory with sliders, plus
 * any other resource (ephemeral-storage, GPUs…) as plain entries.
 */
export function ResourcesMap({ path, root, value, onChange, storage }: { path: Path; root: unknown; value: unknown; onChange: (v: Record<string, string> | undefined) => void; storage?: boolean }) {
  const current = (value as Record<string, unknown> | undefined) ?? {};
  const main: ("cpu" | "memory" | "storage")[] = storage ? ["storage"] : ["cpu", "memory"];
  const others = Object.entries(current).filter(([k]) => !(main as string[]).includes(k));
  const write = (key: string, v: string | undefined) => {
    const next: Record<string, string> = {};
    for (const [k, val] of Object.entries(current)) if (k !== key) next[k] = String(val);
    if (v !== undefined) next[key] = v;
    onChange(Object.keys(next).length ? next : undefined);
  };

  // A limit below the request is rejected by the cluster: say so early.
  const isLimits = path[path.length - 1] === "limits";
  const requests = isLimits ? (getIn(root as never, [...path.slice(0, -1), "requests"]) as Record<string, unknown> | undefined) : undefined;
  const problems = main.flatMap((k) => {
    if (!requests || current[k] === undefined || requests[k] === undefined) return [];
    const scale = SCALES[k];
    const lim = scaleValue(scale, current[k]);
    const req = scaleValue(scale, requests[k]);
    return lim !== null && req !== null && lim < req ? [`The ${k} limit (${current[k]}) is below the request (${requests[k]}); the cluster will reject it.`] : [];
  });

  return (
    <div className="flex flex-col gap-2">
      {main.map((k) => (
        <div key={k} className="flex items-center gap-3">
          <span className="w-16 shrink-0 text-[12px] font-medium text-muted">{k === "cpu" ? "CPU" : k === "memory" ? "Memory" : "Size"}</span>
          <div className="min-w-0 flex-1">
            <QuantitySlider kind={k} label={`${path[path.length - 1]} ${k}`} value={current[k]} onChange={(v) => write(k, v)} />
          </div>
        </div>
      ))}
      {others.map(([k, v]) => (
        <div key={k} className="flex items-center gap-3">
          <span className="w-16 shrink-0 truncate font-mono text-[12px] text-muted" title={k}>
            {k}
          </span>
          <input aria-label={k} className={`${inputClass} w-28 font-mono`} value={String(v ?? "")} onChange={(e) => write(k, e.target.value || undefined)} />
        </div>
      ))}
      {problems.map((p) => (
        <span key={p} className="text-[12px] text-bad">
          {p}
        </span>
      ))}
    </div>
  );
}
