import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Eye, ShieldCheck } from "lucide-react";
import { ReviewDialog } from "../components/ReviewDialog";
import { SchemaForm } from "../components/SchemaForm";
import { YamlEditor } from "../components/YamlEditor";
import { Button, Notice } from "../components/ui";
import { FormService, GuardService, errorMessage, type PlanView, type ResourceRef } from "../lib/api";
import { updateIn, type Path } from "../lib/objpath";
import type { SchemaNode } from "../lib/schema";
import { KINDS, essentialsFor, kindInfo, selectorWarnings, splitApiVersion } from "../lib/templates";
import { nameFromImage } from "../lib/imageref";
import { useClusters } from "../state/clusters";

type Mode = "form" | "yaml";
type Obj = Record<string, unknown>;

const schemaCache = new Map<string, SchemaNode>();

interface EditorPageProps {
  onConnect: () => void;
  initialManifest?: string;
  edit?: ResourceRef;
  /** Start a Deployment for this image. */
  fromImage?: string;
}

/**
 * Create or edit any object with a form or YAML. Both edit the same object;
 * every change is previewed by the Guard before anything is applied.
 */
export function EditorPage({ onConnect, initialManifest, edit, fromImage }: EditorPageProps) {
  const { active, namespace } = useClusters();
  const [yamlText, setYamlText] = useState(initialManifest ?? "");
  const [obj, setObj] = useState<Obj | null>(null);
  const [parseError, setParseError] = useState<string | null>(null);
  const [mode, setMode] = useState<Mode>("form");
  const [schema, setSchema] = useState<SchemaNode | null>(null);
  const [schemaError, setSchemaError] = useState<string | null>(null);
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const [fromTemplate, setFromTemplate] = useState(false);
  const [matching, setMatching] = useState<number | null>(null);
  // Which side changed last, so the other side follows without loops.
  const source = useRef<"yaml" | "form">("yaml");
  const contextName = active?.name ?? "";

  const startFrom = useCallback(
    (kind: string) => {
      const info = KINDS.find((k) => k.kind === kind);
      if (!info?.template) return;
      source.current = "form";
      setFromTemplate(true);
      setDone(null);
      setObj(info.template(namespace || "default"));
    },
    [namespace],
  );

  // Initial content: an object being edited, a manifest, or a starter Deployment.
  useEffect(() => {
    if (edit && contextName) {
      FormService.Object(contextName, edit)
        .then((y) => {
          source.current = "yaml";
          setYamlText(y);
        })
        .catch((err) => setError(errorMessage(err)));
    } else if (fromImage) {
      const info = KINDS.find((k) => k.kind === "Deployment");
      if (info?.template) {
        const name = nameFromImage(fromImage);
        let obj = info.template(namespace || "default") as Obj;
        for (const [path, value] of [
          [["metadata", "name"], name],
          [["metadata", "labels"], { app: name }],
          [["spec", "selector", "matchLabels"], { app: name }],
          [["spec", "template", "metadata", "labels"], { app: name }],
          [["spec", "template", "spec", "containers", 0, "name"], name],
          [["spec", "template", "spec", "containers", 0, "image"], fromImage],
          // A locally loaded image isn't in any registry: don't try to pull it.
          [["spec", "template", "spec", "containers", 0, "imagePullPolicy"], "IfNotPresent"],
        ] as [Path, unknown][]) {
          obj = updateIn(obj, path, value) as Obj;
        }
        source.current = "form";
        setFromTemplate(true);
        setObj(obj);
      }
    } else if (!initialManifest) {
      startFrom("Deployment");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // YAML -> object.
  useEffect(() => {
    if (source.current !== "yaml") return;
    const t = window.setTimeout(() => {
      void FormService.FromYAML(yamlText).then((r) => {
        if (source.current !== "yaml") return;
        if (r.error) setParseError(r.error);
        else {
          setParseError(null);
          setObj(JSON.parse(r.json) as Obj);
        }
      });
    }, 300);
    return () => window.clearTimeout(t);
  }, [yamlText]);

  // Object -> YAML.
  useEffect(() => {
    if (source.current !== "form" || !obj) return;
    const t = window.setTimeout(() => {
      void FormService.ToYAML(JSON.stringify(obj)).then((y) => {
        if (source.current === "form") setYamlText(y);
      });
    }, 250);
    return () => window.clearTimeout(t);
  }, [obj]);

  const apiVersion = typeof obj?.apiVersion === "string" ? obj.apiVersion : "";
  const kind = typeof obj?.kind === "string" ? obj.kind : "";

  // Schema for the object's kind, from the cluster.
  useEffect(() => {
    setSchemaError(null);
    if (!contextName || !apiVersion || !kind) {
      setSchema(null);
      return;
    }
    const key = `${contextName}|${apiVersion}|${kind}`;
    const cached = schemaCache.get(key);
    if (cached) {
      setSchema(cached);
      return;
    }
    setSchema(null);
    const { group, version } = splitApiVersion(apiVersion);
    let cancelled = false;
    FormService.Schema(contextName, group, version, kind)
      .then((json) => {
        const node = JSON.parse(json) as SchemaNode;
        schemaCache.set(key, node);
        if (!cancelled) setSchema(node);
      })
      .catch((err) => !cancelled && setSchemaError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [contextName, apiVersion, kind]);

  // Does a Service's selector match any pods?
  const serviceSelector = kind === "Service" ? ((obj?.spec as Obj | undefined)?.selector as Record<string, string> | undefined) : undefined;
  const objNamespace = ((obj?.metadata as Obj | undefined)?.namespace as string | undefined) || namespace || "default";
  const selectorKey = serviceSelector ? JSON.stringify(serviceSelector) : "";
  useEffect(() => {
    setMatching(null);
    if (!contextName || !selectorKey) return;
    const t = window.setTimeout(() => {
      FormService.MatchingPods(contextName, objNamespace, JSON.parse(selectorKey))
        .then(setMatching)
        .catch(() => setMatching(null));
    }, 600);
    return () => window.clearTimeout(t);
  }, [contextName, objNamespace, selectorKey]);

  const onFormChange = useCallback((path: Path, value: unknown) => {
    source.current = "form";
    setObj((prev) => updateIn(prev ?? {}, path, value) as Obj);
  }, []);

  const warnings = useMemo(() => (obj ? selectorWarnings(obj) : []), [obj]);

  if (!active) {
    return (
      <Notice>
        Connect to a cluster first.{" "}
        <button className="text-accent underline" onClick={onConnect}>
          Go to clusters
        </button>
      </Notice>
    );
  }

  const preview = async (force: boolean) => {
    setBusy(true);
    setError(null);
    setDone(null);
    try {
      let manifest = yamlText;
      if (source.current === "form" && obj) manifest = await FormService.ToYAML(JSON.stringify(obj));
      if (plan) void GuardService.Cancel(plan.id);
      setPlan(await GuardService.PrepareApply(active.name, manifest, force));
    } catch (err) {
      setPlan(null);
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const info = kindInfo(apiVersion, kind);
  const title = edit ? `Edit ${edit.name}` : "Create or edit";

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <h1 className="text-lg font-semibold">{title}</h1>
        {kind && (
          <span className="text-[12px] text-muted">
            {kind} · {apiVersion} · on {active.name}
          </span>
        )}
        <div className="ml-auto flex items-center gap-2">
          {!edit && (
            <select
              aria-label="Start from"
              className="h-8 rounded-md border border-line bg-panel-2 px-2 text-[13px]"
              value=""
              onChange={(e) => e.target.value && startFrom(e.target.value)}
            >
              <option value="">Start from…</option>
              {KINDS.filter((k) => k.template).map((k) => (
                <option key={k.kind} value={k.kind}>
                  {k.label}
                </option>
              ))}
            </select>
          )}
          <div role="tablist" className="flex rounded-md border border-line p-0.5">
            {(["form", "yaml"] as Mode[]).map((m) => (
              <button
                key={m}
                role="tab"
                aria-selected={mode === m}
                onClick={() => setMode(m)}
                className={`rounded px-3 py-1 text-[13px] ${mode === m ? "bg-panel-2 font-medium text-fg" : "text-muted hover:text-fg"}`}
              >
                {m === "form" ? "Form" : "YAML"}
              </button>
            ))}
          </div>
          <Button variant="primary" icon={<Eye className="size-4" />} busy={busy} onClick={() => void preview(false)}>
            Preview change
          </Button>
        </div>
      </div>

      <div className="mb-3 flex flex-col gap-2">
        {done && <Notice tone="good">{done}</Notice>}
        {error && <Notice tone="bad">{error}</Notice>}
        {parseError && <Notice tone="bad">YAML problem: {parseError}</Notice>}
        {warnings.map((w) => (
          <Notice key={w} tone="warn">
            {w}
          </Notice>
        ))}
        {matching !== null &&
          (matching === 0 ? (
            <Notice tone="warn">This Service's selector matches no pods in {objNamespace} right now.</Notice>
          ) : (
            <Notice tone="good">
              The selector matches {matching} {matching === 1 ? "pod" : "pods"} in {objNamespace}.
            </Notice>
          ))}
        {fromTemplate && info?.template && ["Deployment", "Job", "CronJob"].includes(kind) && (
          <Notice icon={<ShieldCheck className="size-4 text-good" />}>
            Starts with secure defaults: runs as non-root, read-only root filesystem, no privilege escalation, and resource limits.
          </Notice>
        )}
      </div>

      <div className="min-h-0 flex-1">
        {mode === "yaml" ? (
          <YamlEditor
            value={yamlText}
            onChange={(v) => {
              source.current = "yaml";
              setYamlText(v);
            }}
          />
        ) : (
          <div className="h-full overflow-y-auto pr-2">
            {parseError && <p className="text-[13px] text-muted">Fix the YAML problem above to use the form.</p>}
            {schemaError && <Notice tone="bad">No form for this kind: {schemaError}. Use the YAML tab.</Notice>}
            {!parseError && !schemaError && !schema && <p className="text-[13px] text-muted">Loading the form…</p>}
            {!parseError && schema && obj && <SchemaForm schema={schema} value={obj} onChange={onFormChange} essentials={essentialsFor(apiVersion, kind)} />}
          </div>
        )}
      </div>

      <ReviewDialog
        plan={plan}
        onClose={() => setPlan(null)}
        onTakeOver={() => void preview(true)}
        onCommitted={(result) => {
          setPlan(null);
          setDone(`${result.message}. Audit entry #${result.auditSeq}.`);
        }}
      />
    </div>
  );
}
