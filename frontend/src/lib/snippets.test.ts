import { describe, expect, it } from "vitest";
import { BUILTIN_SNIPPETS, checkPaste, groupSnippets, matchesSnippet } from "./snippets";

describe("checkPaste", () => {
  it("lets a single line through", () => {
    const c = checkPaste("kubectl get pods", false);
    expect(c.needsConfirm).toBe(false);
    expect(c.lines).toEqual(["kubectl get pods"]);
  });

  it("asks before a paste that would run on its own", () => {
    expect(checkPaste("kubectl get pods\n", false).needsConfirm).toBe(true);
    const c = checkPaste("echo one\r\necho two\r\n", false);
    expect(c.runsImmediately).toBe(true);
    expect(c.lines).toEqual(["echo one", "echo two"]);
  });

  it("flags destructive commands on production", () => {
    const c = checkPaste("kubectl -n shop delete pod web-1", true);
    expect(c.risky).toHaveLength(1);
    expect(c.needsConfirm).toBe(true);
    expect(checkPaste("kubectl -n shop delete pod web-1", false).needsConfirm).toBe(false);
    for (const cmd of ["helm uninstall web", "kubectl scale deploy/web --replicas=0", "kubectl rollout undo deploy/web", "docker system prune -a", "rm -rf /tmp/x", "kubectl drain node-1"]) {
      expect(checkPaste(cmd, true).risky, cmd).toHaveLength(1);
    }
    expect(checkPaste("kubectl get pods --selector=app=deleted", true).risky).toHaveLength(0);
  });
});

describe("snippets", () => {
  it("never end with a newline, so nothing runs by itself", () => {
    for (const s of BUILTIN_SNIPPETS) expect(s.command, s.name).not.toMatch(/[\r\n]/);
  });

  it("group and search", () => {
    const groups = groupSnippets(BUILTIN_SNIPPETS).map(([g]) => g);
    expect(groups[0]).toBe("Kubernetes");
    expect(groups).toContain("Docker");
    expect(BUILTIN_SNIPPETS.filter((s) => matchesSnippet(s, "rollout")).length).toBeGreaterThanOrEqual(2);
  });
});
