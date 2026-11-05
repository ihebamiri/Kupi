import { describe, expect, it } from "vitest";
import { kubectlDelete, kubectlExec, kubectlGet, kubectlLogs, kubectlRestart, kubectlScale, shellArg } from "./kubectl";

const deploy = { context: "k3d-dev", group: "apps", resource: "deployments", namespace: "shop", name: "api" };
const pod = { context: "k3d-dev", group: "", resource: "pods", namespace: "shop", name: "api-1" };

describe("kubectl builders", () => {
  it("lists with the right qualifier and scope", () => {
    expect(kubectlGet({ ...deploy, name: undefined })).toBe("kubectl --context k3d-dev get deployments.apps -n shop");
    expect(kubectlGet({ ...pod, name: undefined, namespace: undefined }, true)).toBe(
      "kubectl --context k3d-dev get pods --all-namespaces",
    );
  });

  it("builds actions", () => {
    expect(kubectlScale(deploy, 3)).toBe("kubectl --context k3d-dev -n shop scale deployments.apps api --replicas=3");
    expect(kubectlRestart(deploy)).toBe("kubectl --context k3d-dev -n shop rollout restart deployments.apps/api");
    expect(kubectlDelete(pod)).toBe("kubectl --context k3d-dev -n shop delete pods api-1");
    expect(kubectlExec(pod, "app")).toBe("kubectl --context k3d-dev -n shop exec -it api-1 -c app -- sh");
    expect(kubectlLogs(pod, { container: "app", follow: true, tail: 500 })).toBe(
      "kubectl --context k3d-dev -n shop logs api-1 -c app -f --tail=500",
    );
  });

  it("quotes unusual arguments", () => {
    expect(shellArg("my context")).toBe("'my context'");
    expect(shellArg("it's")).toBe(`'it'\\''s'`);
    expect(shellArg("arn:aws:eks:eu-west-1:123:cluster/prod")).toBe("arn:aws:eks:eu-west-1:123:cluster/prod");
  });
});
