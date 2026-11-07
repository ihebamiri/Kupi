import { useEffect, useState } from "react";
import { BadgeCheck, ExternalLink, LoaderCircle, Search, Star } from "lucide-react";
import { Badge, Button, Field, Notice, inputClass } from "./ui";
import { AppService, HelmService, errorMessage, type ChartInfo, type ChartPackage, type ChartVersion } from "../lib/api";

export interface ChartChoice {
  repo: string;
  chart: string;
  version: string;
  info: ChartInfo | null;
}

interface ChartPickerProps {
  /** Starts the search with this text (for example the release's chart name). */
  initialQuery?: string;
  value: ChartChoice;
  onChange: (c: ChartChoice) => void;
}

/**
 * Finds a chart: search Artifact Hub, or type a repository address
 * (https://… or oci://…) and a chart name. Then pick a version.
 */
export function ChartPicker({ initialQuery = "", value, onChange }: ChartPickerProps) {
  const [mode, setMode] = useState<"search" | "address">("search");
  const [query, setQuery] = useState(initialQuery);
  const [results, setResults] = useState<ChartPackage[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [versions, setVersions] = useState<ChartVersion[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [repoText, setRepoText] = useState(value.repo);
  const [chartText, setChartText] = useState(value.chart);

  const search = async (q = query) => {
    if (!q.trim()) return;
    setSearching(true);
    setError(null);
    try {
      setResults(await HelmService.SearchCharts(q));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSearching(false);
    }
  };

  useEffect(() => {
    if (initialQuery) void search(initialQuery);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Versions, then the chosen version's details, whenever the chart changes.
  useEffect(() => {
    setVersions(null);
    if (!value.repo || !value.chart) return;
    let cancelled = false;
    setLoading(true);
    HelmService.ChartVersions(value.repo, value.chart)
      .then((vs) => {
        if (cancelled) return;
        setVersions(vs ?? []);
        if (!value.version && vs && vs[0]) onChange({ ...value, version: vs[0].version, info: null });
      })
      .catch((err) => !cancelled && setError(errorMessage(err)))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value.repo, value.chart]);

  useEffect(() => {
    if (!value.repo || !value.chart || !value.version || value.info?.version === value.version) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    HelmService.ChartInfo(value.repo, value.chart, value.version)
      .then((info) => !cancelled && onChange({ ...value, info }))
      .catch((err) => !cancelled && setError(errorMessage(err)))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value.repo, value.chart, value.version]);

  const pick = (p: ChartPackage) => onChange({ repo: p.repoUrl, chart: p.name, version: "", info: null });

  return (
    <div className="flex flex-col gap-3">
      <div role="tablist" aria-label="Find a chart" className="flex w-fit rounded-md border border-line p-0.5">
        {(["search", "address"] as const).map((m) => (
          <button
            key={m}
            role="tab"
            aria-selected={mode === m}
            onClick={() => setMode(m)}
            className={`rounded px-3 py-1 text-[12px] ${mode === m ? "bg-panel-2 font-medium text-fg" : "text-muted hover:text-fg"}`}
          >
            {m === "search" ? "Search Artifact Hub" : "Repository address"}
          </button>
        ))}
      </div>

      {mode === "search" ? (
        <>
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void search();
            }}
          >
            <div className="relative min-w-0 flex-1">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted" aria-hidden />
              <input aria-label="Search charts" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="nginx, postgresql, cert-manager…" className={`${inputClass} w-full pl-8`} />
            </div>
            <Button type="submit" busy={searching}>
              Search
            </Button>
          </form>
          {results && results.length === 0 && <p className="text-[13px] text-muted">No charts found.</p>}
          {results && results.length > 0 && (
            <ul className="max-h-72 divide-y divide-line overflow-y-auto rounded-md border border-line">
              {results.map((p) => {
                const chosen = value.repo === p.repoUrl && value.chart === p.name;
                return (
                  <li key={`${p.repoUrl}/${p.name}`}>
                    <button onClick={() => pick(p)} aria-pressed={chosen} className={`flex w-full flex-col gap-0.5 px-3 py-2 text-left ${chosen ? "bg-accent/12" : "hover:bg-panel-2"}`}>
                      <span className="flex items-center gap-2 text-[13px]">
                        <span className="font-medium">{p.name}</span>
                        <span className="text-muted">{p.repoDisplay || p.repoName}</span>
                        {p.verifiedPublisher && <BadgeCheck className="size-3.5 text-accent" aria-label="Verified publisher" />}
                        {p.official && <Badge className="bg-good/15 text-good">official</Badge>}
                        {p.deprecated && <Badge className="bg-warn/15 text-warn">deprecated</Badge>}
                        <span className="ml-auto flex items-center gap-1 text-[11px] text-muted">
                          <Star className="size-3" aria-hidden /> {p.stars} · {p.version}
                        </span>
                      </span>
                      {p.description && <span className="line-clamp-2 text-[12px] text-muted">{p.description}</span>}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
          <p className="text-[11px] text-muted">Searching sends your search text to artifacthub.io. Nothing else leaves your computer.</p>
        </>
      ) : (
        <form
          className="grid grid-cols-[minmax(0,1fr)_12rem_auto] items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            onChange({ repo: repoText.trim(), chart: chartText.trim(), version: "", info: null });
          }}
        >
          <Field label="Repository">
            <input className={`${inputClass} font-mono`} value={repoText} onChange={(e) => setRepoText(e.target.value)} placeholder="https://charts.example.com or oci://ghcr.io/org/charts" spellCheck={false} />
          </Field>
          <Field label="Chart">
            <input className={`${inputClass} font-mono`} value={chartText} onChange={(e) => setChartText(e.target.value)} placeholder="nginx" spellCheck={false} />
          </Field>
          <Button type="submit">Find versions</Button>
        </form>
      )}

      {error && <Notice tone="bad">{error}</Notice>}

      {value.repo && value.chart && (
        <div className="flex flex-col gap-2 rounded-md border border-line bg-panel-2/50 p-3">
          <div className="flex flex-wrap items-center gap-2 text-[13px]">
            <span className="font-medium">{value.chart}</span>
            <code className="truncate text-[11px] text-muted">{value.repo}</code>
            {loading && <LoaderCircle className="size-3.5 animate-spin text-muted" aria-label="Loading" />}
            <label className="ml-auto flex items-center gap-2 text-[12px] text-muted">
              Version
              <select aria-label="Chart version" className={`${inputClass} h-7 text-[12px]`} value={value.version} onChange={(e) => onChange({ ...value, version: e.target.value, info: null })}>
                {!versions && <option value={value.version}>{value.version || "…"}</option>}
                {(versions ?? []).map((v) => (
                  <option key={v.version} value={v.version}>
                    {v.version}
                    {v.appVersion ? ` (app ${v.appVersion})` : ""}
                    {v.deprecated ? " · deprecated" : ""}
                  </option>
                ))}
              </select>
            </label>
          </div>
          {value.info && (
            <div className="flex flex-col gap-1 text-[12px] text-muted">
              {value.info.description && <span>{value.info.description}</span>}
              <span className="flex flex-wrap gap-x-3">
                {value.info.appVersion && <span>App version {value.info.appVersion}</span>}
                {value.info.kubeVersion && <span>Needs Kubernetes {value.info.kubeVersion}</span>}
                {(value.info.dependencies ?? []).length > 0 && <span>Includes {(value.info.dependencies ?? []).join(", ")}</span>}
                <span className="font-mono">sha256 {value.info.digest.slice(0, 12)}…</span>
              </span>
              {value.info.deprecated && <Notice tone="warn">The maintainers marked this version as deprecated.</Notice>}
              {value.info.home && (
                <button className="flex w-fit items-center gap-1 text-accent hover:underline" title={value.info.home} onClick={() => void AppService.OpenURL(value.info?.home ?? "").catch(() => {})}>
                  <ExternalLink className="size-3" /> Chart home page
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
