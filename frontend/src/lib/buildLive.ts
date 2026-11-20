// Live mode for the Build canvas: after an apply, each block shows what the
// cluster says about its object, refreshed every few seconds.

import { useEffect, useState } from "react";
import type { LiveState } from "../components/BuildNode";
import { CanvasService, errorMessage, type PodSummary } from "./api";
import { SERVED, WORKLOADS, type Block } from "./build";
import type { GraphNode } from "./canvas";

const REFRESH_MS = 2500;
const HEALTH = new Set(["good", "warn", "bad", "unknown"]);

function health(h: string): LiveState["health"] {
  return (HEALTH.has(h) ? h : "unknown") as LiveState["health"];
}

/** A block's live state from its graph node and, for workloads, its pods. */
export function liveState(b: Block, node: GraphNode | undefined, pods: PodSummary[] | undefined): LiveState | null {
  if (!node) return null;
  if (!WORKLOADS.has(b.kind)) return { health: health(node.health), text: node.summary || node.health };
  const list = [...(pods ?? [])].sort((x, y) => rank(x.health) - rank(y.health));
  const bad = list.find((p) => p.health === "bad") ?? list.find((p) => p.health === "warn");
  const text = SERVED.has(b.kind) ? `${node.ready}/${node.pods} ready` : node.summary || `${node.pods} pods`;
  // Copies still starting are progress, not trouble.
  const starting = !bad && node.pods > 0 && node.ready < node.pods;
  return {
    health: starting ? "warn" : health(node.health),
    text: node.pods === 0 && SERVED.has(b.kind) ? "no pods yet" : text,
    pods: list,
    problem: bad && bad.status !== "Running" && bad.status !== "ContainerCreating" && bad.status !== "Pending" ? `${bad.name}: ${bad.status}` : undefined,
  };
}

function rank(h: string): number {
  return h === "bad" ? 0 : h === "warn" ? 1 : h === "good" ? 3 : 2;
}

/** Polls the namespace while live mode is on. */
export function useLive(contextName: string, namespace: string | null, blocks: Block[]) {
  const [states, setStates] = useState<Map<string, LiveState | null>>(new Map());
  const [error, setError] = useState<string | null>(null);
  const [updated, setUpdated] = useState<Date | null>(null);
  const key = blocks.map((b) => `${b.id}:${b.kind}:${b.name}`).join("|");

  useEffect(() => {
    if (!namespace || !contextName) {
      setStates(new Map());
      return;
    }
    let cancelled = false;
    const tick = async () => {
      try {
        const g = await CanvasService.Graph(contextName, namespace);
        const nodes = new Map((g?.nodes ?? []).map((n) => [`${n.kind}/${n.name}`, n as GraphNode]));
        const pods = new Map<string, PodSummary[]>();
        await Promise.all(
          blocks
            .filter((b) => WORKLOADS.has(b.kind) && nodes.has(`${b.kind}/${b.name.trim()}`))
            .map(async (b) => {
              const r = await CanvasService.Pods(contextName, b.kind, namespace, b.name.trim()).catch(() => null);
              pods.set(b.id, r?.pods ?? []);
            }),
        );
        if (cancelled) return;
        setStates(new Map(blocks.map((b) => [b.id, liveState(b, nodes.get(`${b.kind}/${b.name.trim()}`), pods.get(b.id))])));
        setError(null);
        setUpdated(new Date());
      } catch (err) {
        if (!cancelled) setError(errorMessage(err));
      }
    };
    void tick();
    const t = window.setInterval(() => void tick(), REFRESH_MS);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [contextName, namespace, key]);

  return { states, error, updated };
}
