/** A Kubernetes-safe workload name from an image reference: "ghcr.io/org/my_app:1.2" -> "my-app". */
export function nameFromImage(ref: string): string {
  const noDigest = ref.split("@")[0];
  const lastSlash = noDigest.lastIndexOf("/");
  const repo = noDigest.slice(lastSlash + 1);
  const colon = repo.indexOf(":");
  const base = (colon >= 0 ? repo.slice(0, colon) : repo)
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return (base || "app").slice(0, 50);
}

/** True for Docker Hub references (no registry host). */
export function isDockerHub(repo: string): boolean {
  const first = repo.split("/")[0];
  return !(repo.includes("/") && (first.includes(".") || first.includes(":") || first === "localhost"));
}
