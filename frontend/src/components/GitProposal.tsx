import { useEffect, useState } from "react";
import { ExternalLink, FolderGit2, GitPullRequest } from "lucide-react";
import { Dialog } from "./Dialog";
import { DiffView } from "./DiffView";
import { Badge, Button, Field, Notice, inputClass } from "./ui";
import { AppService, GitOpsService, errorMessage, type GitHubStatus, type GitOpsOwnerInfo, type ProposalView, type ResourceRef } from "../lib/api";

/**
 * For an object Argo CD or Flux manages: says that edits here get reverted,
 * and offers to propose the change as a pull request instead.
 */
export function GitOpsEditNotice({ contextName, target, manifest }: { contextName: string; target: ResourceRef; manifest: () => Promise<string> }) {
  const [owner, setOwner] = useState<GitOpsOwnerInfo | null>(null);
  const [proposal, setProposal] = useState<ProposalView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [opened, setOpened] = useState<string | null>(null);

  useEffect(() => {
    GitOpsService.GitSource(contextName, target)
      .then((o) => setOwner(o ?? null))
      .catch(() => setOwner(null));
  }, [contextName, target]);

  if (!owner) return null;
  // Go sends the tool's display name: "Argo CD" or "Flux".
  const tool = owner.owner.tool;

  const propose = async () => {
    setBusy(true);
    setError(null);
    try {
      setProposal(await GitOpsService.ProposeChange(contextName, target, await manifest()));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Notice tone="warn" icon={<GitPullRequest className="size-4 text-warn" />}>
        <p>
          Managed by {tool} {owner.owner.kind} <b>{owner.owner.name}</b>: changes applied here are reverted at the next sync.
          {owner.canPropose ? " Propose the change in Git instead, as a pull request." : owner.why ? ` ${owner.why}` : ""}
        </p>
        {owner.canPropose && (
          <Button className="mt-2" busy={busy} icon={<GitPullRequest className="size-4" />} onClick={() => void propose()}>
            Propose in Git
          </Button>
        )}
        {error && <p className="mt-2 text-bad">{error}</p>}
        {opened && (
          <p className="mt-2">
            Pull request opened:{" "}
            <button className="text-accent underline" onClick={() => void AppService.OpenURL(opened)}>
              {opened}
            </button>
          </p>
        )}
      </Notice>
      {proposal && (
        <ProposalDialog
          proposal={proposal}
          onClose={() => setProposal(null)}
          onOpened={(url) => {
            setProposal(null);
            setOpened(url);
          }}
        />
      )}
    </>
  );
}

function ProposalDialog({ proposal, onClose, onOpened }: { proposal: ProposalView; onClose: () => void; onOpened: (url: string) => void }) {
  const [title, setTitle] = useState(proposal.title);
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const open = async () => {
    setBusy(true);
    setError(null);
    try {
      onOpened(await GitOpsService.OpenPullRequest(proposal.id, title, body));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      wide
      onOpenChange={(o) => !o && onClose()}
      title="Open a pull request?"
      description={`On GitHub, in ${proposal.repo}. Nothing changes in the cluster; ${proposal.base} gets the change once the pull request is merged and synced.`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" busy={busy} icon={<GitPullRequest className="size-4" />} disabled={!title.trim()} onClick={() => void open()}>
            Open pull request
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2 text-[12px]">
          <Badge className="bg-panel-2 text-muted">{proposal.repo}</Badge>
          <Badge className="bg-panel-2 text-muted">into {proposal.base}</Badge>
          <button className="flex items-center gap-1 font-mono text-accent hover:underline" title={proposal.fileUrl} onClick={() => void AppService.OpenURL(proposal.fileUrl)}>
            {proposal.file} <ExternalLink className="size-3" />
          </button>
        </div>
        {(proposal.warnings ?? []).map((w) => (
          <Notice key={w} tone="warn">
            {w}
          </Notice>
        ))}
        <DiffView diff={proposal.diff} />
        <Field label="Title">
          <input className={inputClass} value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label="Description (optional)">
          <textarea className={`${inputClass} h-20 py-1.5`} value={body} maxLength={10000} onChange={(e) => setBody(e.target.value)} placeholder="Why this change is needed" />
        </Field>
        <p className="text-[12px] text-muted">Kupi creates a branch named kupi/…, commits this one file to it, and opens the pull request with your saved GitHub token.</p>
        {error && <Notice tone="bad">{error}</Notice>}
      </div>
    </Dialog>
  );
}

/** Save or forget the GitHub token used for pull requests. */
export function GitHubConnection() {
  const [status, setStatus] = useState<GitHubStatus | null>(null);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = () => void GitOpsService.GitHubStatus().then(setStatus);
  useEffect(load, []);

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      setStatus(await GitOpsService.SaveGitHubToken(token));
      setToken("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="rounded-lg border border-line bg-panel p-4">
      <h2 className="mb-1 flex items-center gap-2 text-[14px] font-semibold">
        <FolderGit2 className="size-4" aria-hidden /> GitHub
      </h2>
      <p className="mb-3 text-[12px] text-muted">
        To change an object that Argo CD or Flux manages, Kupi can open a pull request in the app's GitHub repository. It needs a token with access to the repository's contents and pull
        requests (a fine-grained token, or a classic token with the repo scope), kept encrypted in your vault.
      </p>
      {status?.vaultLocked && <Notice tone="warn">Unlock the vault to use or save a GitHub token.</Notice>}
      {status?.connected ? (
        <div className="flex items-center gap-3 text-[13px]">
          <Badge className="bg-good/15 text-good">Connected</Badge>
          <span>
            as <b>{status.login}</b>
          </span>
          <Button
            className="ml-auto"
            variant="ghost"
            onClick={() =>
              void GitOpsService.ForgetGitHubToken()
                .then(load)
                .catch((err) => setError(errorMessage(err)))
            }
          >
            Forget token
          </Button>
        </div>
      ) : (
        !status?.vaultLocked && (
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void save();
            }}
          >
            <input type="password" aria-label="GitHub token" className={`${inputClass} min-w-0 flex-1 font-mono`} value={token} onChange={(e) => setToken(e.target.value)} placeholder="github_pat_… or ghp_…" autoComplete="off" spellCheck={false} />
            <Button type="submit" variant="primary" busy={busy} disabled={!token}>
              Check and save
            </Button>
          </form>
        )
      )}
      {error && (
        <div className="mt-2">
          <Notice tone="bad">{error}</Notice>
        </div>
      )}
    </section>
  );
}
