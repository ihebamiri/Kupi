import { useEffect, useState } from "react";
import { BadgeCheck, KeyRound, ShieldQuestion, Trash2 } from "lucide-react";
import { Badge, Button, Field, Notice, inputClass } from "./ui";
import { ImageService, errorMessage, type RegistryLogin, type ScanReport, type SignatureReport } from "../lib/api";
import { age } from "../lib/format";

const SEVERITY: Record<string, string> = {
  CRITICAL: "bg-bad/20 text-bad",
  HIGH: "bg-bad/12 text-bad",
  MEDIUM: "bg-warn/15 text-warn",
  LOW: "bg-panel-2 text-muted",
  UNKNOWN: "bg-panel-2 text-muted",
};

/** Vulnerability scan (Trivy) and signature check (cosign) for one image. */
export function ImageSecurity({ refName }: { refName: string }) {
  const [tools, setTools] = useState<Record<string, boolean> | null>(null);
  const [scan, setScan] = useState<ScanReport | null>(null);
  const [sig, setSig] = useState<SignatureReport | null>(null);
  const [busy, setBusy] = useState<"scan" | "verify" | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void ImageService.ScanTools().then((t) => setTools({ trivy: !!t?.trivy, cosign: !!t?.cosign }));
  }, []);

  const runScan = async () => {
    setBusy("scan");
    setError(null);
    try {
      setScan(await ImageService.Scan(refName));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };
  const runVerify = async () => {
    setBusy("verify");
    setError(null);
    try {
      setSig(await ImageService.VerifySignature(refName));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  const total = scan ? Object.values(scan.counts ?? {}).reduce<number>((a, b) => a + (b ?? 0), 0) : 0;

  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-[12px] font-semibold text-muted">Security</h3>
      <div className="flex flex-wrap gap-2">
        <Button busy={busy === "scan"} disabled={tools?.trivy === false} onClick={() => void runScan()} title={tools?.trivy === false ? "Install Trivy to scan images" : "Look for known vulnerabilities with Trivy"}>
          Scan for vulnerabilities
        </Button>
        <Button busy={busy === "verify"} disabled={tools?.cosign === false} onClick={() => void runVerify()} title={tools?.cosign === false ? "Install cosign to check signatures" : "Check the image's Sigstore signature with cosign"}>
          Check signature
        </Button>
      </div>
      {tools && (!tools.trivy || !tools.cosign) && (
        <p className="text-[12px] text-muted">
          {!tools.trivy && "Scanning uses Trivy (trivy.dev). "}
          {!tools.cosign && "Signature checks use cosign (sigstore.dev). "}
          Install {!tools.trivy && !tools.cosign ? "them" : "it"} and reopen this panel; Kupi runs {!tools.trivy && !tools.cosign ? "them" : "it"} on this computer, nothing is uploaded.
        </p>
      )}
      {busy === "scan" && <p className="text-[12px] text-muted">The first scan downloads Trivy's vulnerability database and can take a few minutes.</p>}
      {error && <Notice tone="bad">{error}</Notice>}

      {sig &&
        (sig.signed ? (
          <Notice tone="good" icon={<BadgeCheck className="size-4 text-good" />}>
            {(sig.signatures ?? []).length > 0 ? (
              <>
                <p className="font-medium">Signed. Check that the signer is the publisher you expect:</p>
                <ul className="mt-1 flex flex-col gap-0.5 font-mono text-[12px] break-all">
                  {(sig.signatures ?? []).map((s, i) => (
                    <li key={i}>
                      {s.identity || "(no identity)"} <span className="text-muted">via {s.issuer || "unknown issuer"}</span>
                    </li>
                  ))}
                </ul>
              </>
            ) : (
              <p className="font-medium">Signed, but cosign didn't say by whom. Check the publisher's documentation for how they sign.</p>
            )}
          </Notice>
        ) : (
          <Notice tone="warn" icon={<ShieldQuestion className="size-4 text-warn" />}>
            Not verified: {(sig.problem || "no signature found").replace(/\.$/, "")}. Images that only exist on this computer can't be checked; signatures live in the registry.
          </Notice>
        ))}

      {scan && (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2 text-[12px]">
            {total === 0 ? (
              <Badge className="bg-good/15 text-good">No known vulnerabilities</Badge>
            ) : (
              ["CRITICAL", "HIGH", "MEDIUM", "LOW", "UNKNOWN"].map((s) =>
                scan.counts?.[s] ? (
                  <Badge key={s} className={SEVERITY[s]}>
                    {scan.counts[s]} {s.toLowerCase()}
                  </Badge>
                ) : null,
              )
            )}
            {scan.fixable > 0 && <span className="text-muted">{scan.fixable} have a fixed version</span>}
            <span className="ml-auto text-muted">
              {scan.scanner}
              {scan.os && ` · ${scan.os}`} · {age(scan.scanned)} ago
            </span>
          </div>
          {(scan.findings ?? []).length > 0 && (
            <div className="max-h-72 overflow-auto rounded-md border border-line">
              <table className="w-full text-[12px]">
                <thead className="sticky top-0 bg-panel text-left text-[11px] text-muted">
                  <tr>
                    <th className="px-2 py-1.5 font-medium">Severity</th>
                    <th className="px-2 py-1.5 font-medium">ID</th>
                    <th className="px-2 py-1.5 font-medium">Package</th>
                    <th className="px-2 py-1.5 font-medium">Installed</th>
                    <th className="px-2 py-1.5 font-medium">Fixed in</th>
                  </tr>
                </thead>
                <tbody>
                  {(scan.findings ?? []).map((f, i) => (
                    <tr key={i} className="border-t border-line/60" title={f.title}>
                      <td className="px-2 py-1">
                        <Badge className={SEVERITY[f.severity] ?? SEVERITY.UNKNOWN}>{f.severity.toLowerCase()}</Badge>
                      </td>
                      <td className="px-2 py-1 font-mono">{f.id}</td>
                      <td className="px-2 py-1">{f.package}</td>
                      <td className="px-2 py-1 font-mono text-muted">{f.installed}</td>
                      <td className={`px-2 py-1 font-mono ${f.fixed ? "text-good" : "text-muted"}`}>{f.fixed || "no fix yet"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {scan.truncated && <p className="text-[12px] text-muted">Showing the {(scan.findings ?? []).length} most serious findings.</p>}
        </div>
      )}
    </section>
  );
}

/** Logins for private registries, kept in the vault and used for pulls. */
export function RegistryLogins() {
  const [logins, setLogins] = useState<RegistryLogin[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [host, setHost] = useState("");
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () =>
    ImageService.RegistryLogins()
      .then((l) => {
        setLogins(l ?? []);
        setError(null);
      })
      .catch((err) => setError(errorMessage(err)));
  useEffect(() => {
    void load();
  }, []);

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      await ImageService.SaveRegistryLogin(host, user, password);
      setHost("");
      setUser("");
      setPassword("");
      await load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex max-w-3xl flex-col gap-4">
      <p className="text-[13px] text-muted">
        Logins for private registries, used when Kupi pulls an image from them. They're kept encrypted in your <b>vault</b>, which must be unlocked; Kupi never shows the password again.
        Pulls from registries without a login are anonymous.
      </p>
      {error && <Notice tone={error.includes("unlock") ? "warn" : "bad"}>{error}</Notice>}
      {logins && (
        <ul className="divide-y divide-line rounded-md border border-line">
          {logins.length === 0 && <li className="px-3 py-2 text-[13px] text-muted">No registry logins yet.</li>}
          {logins.map((l) => (
            <li key={l.host} className="flex items-center gap-3 px-3 py-2 text-[13px]">
              <KeyRound className="size-4 text-muted" aria-hidden />
              <span className="font-medium">{l.host}</span>
              <span className="text-muted">as {l.username}</span>
              <span className="ml-auto text-[12px] text-muted">saved {age(l.updated)} ago</span>
              <Button
                variant="ghost"
                aria-label={`Delete the login for ${l.host}`}
                icon={<Trash2 className="size-3.5" />}
                onClick={() =>
                  void ImageService.DeleteRegistryLogin(l.host)
                    .then(load)
                    .catch((err) => setError(errorMessage(err)))
                }
              />
            </li>
          ))}
        </ul>
      )}
      <form
        className="grid grid-cols-3 items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <Field label="Registry">
          <input className={`${inputClass} font-mono`} value={host} onChange={(e) => setHost(e.target.value)} placeholder="ghcr.io" spellCheck={false} autoComplete="off" />
        </Field>
        <Field label="User name">
          <input className={inputClass} value={user} onChange={(e) => setUser(e.target.value)} spellCheck={false} autoComplete="off" />
        </Field>
        <Field label="Password or access token">
          <input type="password" className={`${inputClass} font-mono`} value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" spellCheck={false} />
        </Field>
        <div className="col-span-3">
          <Button type="submit" variant="primary" busy={busy} disabled={!host || !user || !password}>
            Save login
          </Button>
        </div>
      </form>
      <p className="text-[12px] text-muted">For GitHub (ghcr.io), use a personal access token with the read:packages scope instead of your password.</p>
    </div>
  );
}
