import { useMemo, useState, type ReactNode } from "react";
import { ChevronDown, ChevronRight, Plus, Trash2 } from "lucide-react";
import { getIn, pathLabel, type Path } from "../lib/objpath";
import { firstSentence, matchesSearch, nodeAt, type SchemaNode } from "../lib/schema";
import { referenceFor, sliderFor } from "../lib/refs";
import type { Essential } from "../lib/templates";
import { CountSlider, FormEnvContext, QuantitySlider, RefInput, ResourcesMap, type FormEnv } from "./FormInputs";
import { inputClass } from "./ui";

type OnChange = (path: Path, value: unknown) => void;

interface SchemaFormProps {
  schema: SchemaNode;
  value: unknown;
  onChange: OnChange;
  essentials: Essential[];
  /** Cluster and default namespace for suggesting existing objects. */
  env: FormEnv;
}

/**
 * Form generated from the cluster's own schema. Common fields come first with
 * plain labels; "All fields" reaches everything the schema allows. Fields that
 * name another object suggest the ones that exist; sizes and counts get sliders.
 */
export function SchemaForm({ schema, value, onChange, essentials, env }: SchemaFormProps) {
  const [showAll, setShowAll] = useState(false);
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();

  return (
    <FormEnvContext.Provider value={env}>
    <div className="flex flex-col gap-5">
      <section className="flex flex-col gap-3">
        {essentials.map((e) => {
          const node = nodeAt(schema, e.path);
          if (!node) return null;
          return (
            <FieldRow key={pathLabel(e.path)} label={e.label} hint={e.hint ?? firstSentence(node.description)} path={e.path} full={node.description}>
              <Control node={node} path={e.path} value={value} onChange={onChange} label={e.label} />
            </FieldRow>
          );
        })}
      </section>

      <section className="rounded-lg border border-line">
        <div className="flex items-center gap-2 border-b border-line px-3 py-2">
          <button className="flex items-center gap-1 text-[13px] font-medium" onClick={() => setShowAll((s) => !s)} aria-expanded={showAll || q !== ""}>
            {showAll || q ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
            All fields
          </button>
          <input
            aria-label="Search fields"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search fields, e.g. tolerations"
            className={`${inputClass} ml-auto h-7 w-64 text-[12px]`}
          />
        </div>
        {(showAll || q) && (
          <div className="p-3">
            <ObjectFields node={schema} path={[]} value={value} onChange={onChange} query={q} />
          </div>
        )}
      </section>
    </div>
    </FormEnvContext.Provider>
  );
}

function FieldRow({ label, hint, path, full, required, children }: { label: string; hint?: string; path: Path; full?: string; required?: boolean; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline gap-2">
        <span className="text-[13px] font-medium">
          {label}
          {required && <span className="ml-0.5 text-bad" aria-label="required">*</span>}
        </span>
        <code className="truncate text-[11px] text-muted">{pathLabel(path)}</code>
      </div>
      {children}
      {hint && (
        <span className="text-[12px] text-muted" title={full}>
          {hint}
        </span>
      )}
    </div>
  );
}

function ObjectFields({ node, path, value, onChange, query }: { node: SchemaNode; path: Path; value: unknown; onChange: OnChange; query: string }) {
  const fields = (node.fields ?? []).filter((f) => !(path.length === 0 && (f.name === "apiVersion" || f.name === "kind")));
  return (
    <div className="flex flex-col gap-3">
      {fields
        .filter((f) => matchesSearch(f.name, f.node, query))
        .map((f) => {
          const childPath = [...path, f.name];
          if (f.node.type === "object" || (f.node.type === "array" && f.node.items?.type === "object")) {
            return <Group key={f.name} name={f.name} node={f.node} path={childPath} value={value} onChange={onChange} query={query} required={f.required} />;
          }
          return (
            <FieldRow key={f.name} label={f.name} hint={firstSentence(f.node.description)} full={f.node.description} path={childPath} required={f.required}>
              <Control node={f.node} path={childPath} value={value} onChange={onChange} />
            </FieldRow>
          );
        })}
    </div>
  );
}

function Group({ name, node, path, value, onChange, query, required }: { name: string; node: SchemaNode; path: Path; value: unknown; onChange: OnChange; query: string; required?: boolean }) {
  const [open, setOpen] = useState(false);
  const has = getIn(value, path) !== undefined;
  const expanded = open || (query !== "" && !name.toLowerCase().includes(query));
  return (
    <div className="rounded-md border border-line/70">
      <button className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[13px]" onClick={() => setOpen((o) => !o)} aria-expanded={expanded}>
        {expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
        <span className="font-medium">{name}</span>
        {required && <span className="text-bad">*</span>}
        {has && <span className="size-1.5 rounded-full bg-accent" aria-label="has a value" />}
        <span className="ml-2 truncate text-[12px] text-muted" title={node.description}>
          {firstSentence(node.description)}
        </span>
      </button>
      {expanded && (
        <div className="border-t border-line/70 px-3 py-2.5">
          {node.type === "array" ? (
            <Control node={node} path={path} value={value} onChange={onChange} query={query} />
          ) : (
            <ObjectFields node={node} path={path} value={value} onChange={onChange} query={query} />
          )}
        </div>
      )}
    </div>
  );
}

function Control({ node, path, value, onChange, query = "", label }: { node: SchemaNode; path: Path; value: unknown; onChange: OnChange; query?: string; label?: string }) {
  const current = getIn(value, path);
  // Every input is named for screen readers: the friendly label, or the field path.
  const name = label ?? pathLabel(path);
  const kind = typeof (value as { kind?: unknown } | null)?.kind === "string" ? (value as { kind: string }).kind : "";
  switch (node.type) {
    case "string": {
      const ref = node.enum && node.enum.length > 0 ? null : referenceFor(path, value);
      if (ref) return <RefInput label={name} value={current} onChange={(v) => onChange(path, v)} refSpec={ref} root={value} />;
      if (node.enum && node.enum.length > 0) {
        return (
          <select aria-label={name} className={inputClass} value={typeof current === "string" ? current : ""} onChange={(e) => onChange(path, e.target.value || undefined)}>
            <option value="">(not set)</option>
            {node.enum.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        );
      }
      return <TextInput label={name} value={current} onChange={(v) => onChange(path, v)} />;
    }
    case "quantity": {
      const slider = sliderFor(path, kind);
      if (slider && slider !== "replicas") return <QuantitySlider kind={slider} label={name} value={current} onChange={(v) => onChange(path, v)} />;
      return <TextInput label={name} value={current} placeholder="e.g. 250m, 512Mi, 1Gi" onChange={(v) => onChange(path, v)} />;
    }
    case "intOrString":
      return <TextInput label={name} value={current} placeholder="number or name" onChange={(v) => onChange(path, v === undefined ? undefined : /^\d+$/.test(v) ? Number(v) : v)} />;
    case "integer":
    case "number":
      if (node.type === "integer" && sliderFor(path, kind) === "replicas") {
        return <CountSlider label={name} value={current} onChange={(v) => onChange(path, v)} />;
      }
      return (
        <input
          aria-label={name}
          className={inputClass}
          inputMode="numeric"
          value={current === undefined || current === null ? "" : String(current)}
          placeholder="(not set)"
          onChange={(e) => {
            const raw = e.target.value.trim();
            if (raw === "") onChange(path, undefined);
            else if (!Number.isNaN(Number(raw))) onChange(path, node.type === "integer" ? Math.trunc(Number(raw)) : Number(raw));
          }}
        />
      );
    case "boolean":
      return (
        <select aria-label={name} className={inputClass} value={current === true ? "true" : current === false ? "false" : ""} onChange={(e) => onChange(path, e.target.value === "" ? undefined : e.target.value === "true")}>
          <option value="">(not set)</option>
          <option value="true">true</option>
          <option value="false">false</option>
        </select>
      );
    case "map": {
      const last = path[path.length - 1];
      if ((last === "requests" || last === "limits") && path[path.length - 2] === "resources" && node.values?.type === "quantity") {
        return <ResourcesMap path={path} root={value} value={current} storage={kind === "PersistentVolumeClaim" || path[0] === "spec" && path[1] === "volumeClaimTemplates"} onChange={(v) => onChange(path, v)} />;
      }
      return <MapEditor value={current} onChange={(v) => onChange(path, v)} />;
    }
    case "array":
      if (node.items?.type === "object") return <ObjectList node={node.items} path={path} value={value} onChange={onChange} query={query} />;
      return <StringList value={current} onChange={(v) => onChange(path, v)} numeric={node.items?.type === "integer"} />;
    case "object":
      return <ObjectFields node={node} path={path} value={value} onChange={onChange} query={query} />;
    default:
      return <JsonInput value={current} onChange={(v) => onChange(path, v)} />;
  }
}

function TextInput({ value, onChange, placeholder, label }: { value: unknown; onChange: (v: string | undefined) => void; placeholder?: string; label: string }) {
  return (
    <input
      aria-label={label}
      className={inputClass}
      value={value === undefined || value === null ? "" : String(value)}
      placeholder={placeholder ?? "(not set)"}
      spellCheck={false}
      onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)}
    />
  );
}

function MapEditor({ value, onChange }: { value: unknown; onChange: (v: Record<string, string> | undefined) => void }) {
  const entries = Object.entries((value as Record<string, unknown>) ?? {});
  const write = (next: [string, unknown][]) => onChange(next.length ? Object.fromEntries(next.map(([k, v]) => [k, String(v ?? "")])) : undefined);
  return (
    <div className="flex flex-col gap-1.5">
      {entries.map(([k, v], i) => (
        <div key={i} className="flex gap-1.5">
          <input
            aria-label="Key"
            className={`${inputClass} w-2/5 font-mono`}
            value={k}
            onChange={(e) => write(entries.map((en, j) => (j === i ? [e.target.value, en[1]] : en)))}
          />
          <input
            aria-label={`Value for ${k}`}
            className={`${inputClass} flex-1 font-mono`}
            value={String(v ?? "")}
            onChange={(e) => write(entries.map((en, j) => (j === i ? [en[0], e.target.value] : en)))}
          />
          <button className="rounded px-1.5 text-muted hover:text-bad" aria-label={`Remove ${k}`} onClick={() => write(entries.filter((_, j) => j !== i))}>
            <Trash2 className="size-4" />
          </button>
        </div>
      ))}
      <button className="flex w-fit items-center gap-1 text-[12px] text-accent" onClick={() => write([...entries, [`key${entries.length + 1}`, ""]])}>
        <Plus className="size-3.5" /> Add
      </button>
    </div>
  );
}

function StringList({ value, onChange, numeric }: { value: unknown; onChange: (v: unknown[] | undefined) => void; numeric?: boolean }) {
  const items = Array.isArray(value) ? value : [];
  const write = (next: unknown[]) => onChange(next.length ? next : undefined);
  const parse = (s: string) => (numeric && /^-?\d+$/.test(s) ? Number(s) : s);
  return (
    <div className="flex flex-col gap-1.5">
      {items.map((v, i) => (
        <div key={i} className="flex gap-1.5">
          <input
            aria-label={`Item ${i + 1}`}
            className={`${inputClass} flex-1 font-mono`}
            value={String(v ?? "")}
            onChange={(e) => write(items.map((x, j) => (j === i ? parse(e.target.value) : x)))}
          />
          <button className="rounded px-1.5 text-muted hover:text-bad" aria-label={`Remove item ${i + 1}`} onClick={() => write(items.filter((_, j) => j !== i))}>
            <Trash2 className="size-4" />
          </button>
        </div>
      ))}
      <button className="flex w-fit items-center gap-1 text-[12px] text-accent" onClick={() => write([...items, ""])}>
        <Plus className="size-3.5" /> Add
      </button>
    </div>
  );
}

function ObjectList({ node, path, value, onChange, query }: { node: SchemaNode; path: Path; value: unknown; onChange: OnChange; query: string }) {
  const items = (getIn(value, path) as unknown[] | undefined) ?? [];
  const titles = useMemo(
    () => items.map((it, i) => {
      const o = it as Record<string, unknown> | null;
      const name = o && typeof o === "object" ? (o.name ?? o.key ?? o.containerPort ?? o.port) : undefined;
      return name !== undefined ? String(name) : `#${i + 1}`;
    }),
    [items],
  );
  return (
    <div className="flex flex-col gap-2">
      {items.map((_, i) => (
        <div key={i} className="rounded-md border border-line/70">
          <div className="flex items-center gap-2 border-b border-line/70 px-2.5 py-1 text-[12px]">
            <span className="font-medium">{titles[i]}</span>
            <button className="ml-auto rounded px-1 text-muted hover:text-bad" aria-label={`Remove ${titles[i]}`} onClick={() => onChange([...path, i], undefined)}>
              <Trash2 className="size-3.5" />
            </button>
          </div>
          <div className="px-3 py-2">
            <ObjectFields node={node} path={[...path, i]} value={value} onChange={onChange} query={query} />
          </div>
        </div>
      ))}
      <button className="flex w-fit items-center gap-1 text-[12px] text-accent" onClick={() => onChange([...path, items.length], {})}>
        <Plus className="size-3.5" /> Add item
      </button>
    </div>
  );
}

function JsonInput({ value, onChange }: { value: unknown; onChange: (v: unknown) => void }) {
  const [text, setText] = useState(value === undefined ? "" : JSON.stringify(value, null, 2));
  const [error, setError] = useState<string | null>(null);
  return (
    <div className="flex flex-col gap-1">
      <textarea
        className={`${inputClass} h-24 py-1.5 font-mono text-[12px]`}
        value={text}
        spellCheck={false}
        placeholder="JSON, or edit this part in the YAML tab"
        onChange={(e) => setText(e.target.value)}
        onBlur={() => {
          if (text.trim() === "") {
            setError(null);
            onChange(undefined);
            return;
          }
          try {
            onChange(JSON.parse(text));
            setError(null);
          } catch {
            setError("Not valid JSON. Edit this part in the YAML tab instead.");
          }
        }}
      />
      {error && <span className="text-[12px] text-bad">{error}</span>}
    </div>
  );
}
