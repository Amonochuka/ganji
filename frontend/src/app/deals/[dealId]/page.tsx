// @ts-nocheck
"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { StatusIcon } from "@/components/ui/status-icon";
import { DealCard } from "@/components/deals/deal-card";
import { WalletLinkDialog } from "@/components/deals/wallet-link-dialog";
import {
  api,
  type Artifact,
  type ArtifactKind,
  type DealView,
  type DealStatus,
  type DealDetailResponse,
  type SubmitWorkResponse,
  type MilestoneView,
} from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";
import { formatCurrency } from "@/lib/utils/format";
import { copyText } from "@/lib/utils/clipboard";
import {
  DealHeader,
  StatusTimeline,
  getMilestoneStatusClass,
  getMilestoneStatusLabel,
  getStatusClass,
  getStatusLabel,
} from "./deal-ui";

export default function DealDetailPage() {
  const router = useRouter();
  const params = useParams();
  const { tokens, user, logout, isLoading: authLoading } = useAuth();
  const dealId = params.dealId as string;

  const [deal, setDeal] = useState<DealView | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [showBanner, setShowBanner] = useState(false);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [milestones, setMilestones] = useState<MilestoneView[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [workLink, setWorkLink] = useState("");
  const [showPayeeInvoiceForm, setShowPayeeInvoiceForm] = useState(false);
  const [payeeInvoiceInput, setPayeeInvoiceInput] = useState("");
  const [showPayeeWalletDialog, setShowPayeeWalletDialog] = useState(false);
  const [payeeWalletURL, setPayeeWalletURL] = useState("");
  const [payeeWalletKey, setPayeeWalletKey] = useState("");
  const [payeeWalletError, setPayeeWalletError] = useState<string | null>(null);
  const [isGeneratingPayeeInvoice, setIsGeneratingPayeeInvoice] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [artifactKind, setArtifactKind] = useState<ArtifactKind>("source_file");
  const [roleHint, setRoleHint] = useState<"client" | "freelancer" | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const value = new URLSearchParams(window.location.search).get("role");
    setRoleHint(value === "client" || value === "freelancer" ? value : null);
  }, []);

  useEffect(() => {
    if (authLoading) return;
    if (!tokens) {
      router.push("/login");
      return;
    }
    fetchDeal();
    fetchMilestones();
    fetchArtifacts();
  }, [authLoading, tokens, dealId, router]);

  async function fetchDeal(silent = false) {
    if (!tokens) return;
    try {
      if (!silent) setIsLoading(true);
      const res = await api.getDeal(dealId, tokens.access_token);
      setDeal(res.deal);
      if (!silent) setLoadError(null);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Failed to load deal";
      if (!silent) setLoadError(message);
      else setActionError(message);
    } finally {
      if (!silent) setIsLoading(false);
    }
  }

  async function fetchMilestones() {
    if (!tokens) return;
    try {
      const res = await api.listMilestones(dealId, tokens.access_token);
      setMilestones(res.milestones);
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : "Failed to load milestones",
      );
    }
  }

  async function fetchArtifacts() {
    if (!tokens) return;
    try {
      const res = await api.listArtifacts(dealId, tokens.access_token);
      setArtifacts(res.artifacts ?? []);
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : "Failed to load deliverables",
      );
    }
  }

  async function handleAction(action: string, fn: () => Promise<void>) {
    setActionError(null);
    setNotice(null);
    setActionLoading(action);
    try {
      await fn();
      await fetchDeal();
      await fetchMilestones();
      await fetchArtifacts();
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : `Failed to ${action}`,
      );
    } finally {
      setActionLoading(null);
    }
  }

  async function handleSubmitWork() {
    await handleAction("submit work", async () => {
      const res = await api.submitWork(dealId, tokens!.access_token);
      setDeal(res.deal);
    });
  }

  async function handleAttachWorkLink() {
    const value = workLink.trim();
    if (!value) {
      setActionError("Enter a work link first");
      return;
    }
    const file = new File([value], "work-link.txt", { type: "text/plain" });
    await handleAction("attach work link", async () => {
      await api.uploadArtifact(
        dealId,
        "source_file",
        file,
        tokens!.access_token,
      );
      setWorkLink("");
    });
  }

  async function handleUploadFile() {
    if (!selectedFile) {
      setActionError("Choose a file to upload");
      return;
    }
    const file = selectedFile;
    await handleAction("upload artifact", async () => {
      await api.uploadArtifact(
        dealId,
        artifactKind,
        file,
        tokens!.access_token,
      );
      setSelectedFile(null);
      if (fileInputRef.current) fileInputRef.current.value = "";
    });
  }

  async function handleDeleteArtifact(artifactId: string) {
    if (!window.confirm("Remove this deliverable?")) return;
    await handleAction("delete artifact", async () => {
      await api.deleteArtifact(dealId, artifactId, tokens!.access_token);
    });
  }

  async function handleDownloadArtifact(artifact: Artifact) {
    await handleAction("download artifact", async () => {
      const blob = await api.downloadArtifact(
        dealId,
        artifact.id,
        tokens!.access_token,
      );
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = artifact.storage_key.split("/").pop() || "artifact";
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    });
  }

  async function handleRotateShareLink() {
    await handleAction("rotate share link", async () => {
      const res = await api.rotateShareLink(dealId, tokens!.access_token);
      setDeal(res.deal);
    });
  }

  async function handleUpdatePayeeInvoice() {
    const invoice = payeeInvoiceInput.trim();
    if (!invoice.startsWith("lnbc")) {
      setActionError("Payee invoice must be a valid Lightning invoice");
      return;
    }
    await handleAction("update payee invoice", async () => {
      const res = await api.updatePayeeInvoice(
        dealId,
        { payee_invoice: invoice },
        tokens!.access_token,
      );
      setDeal(res.deal);
      setShowPayeeInvoiceForm(false);
      setNotice("Payee invoice updated. Client is not charged again; escrow funds remain held.");
    });
  }

  async function handleGeneratePayeeInvoice() {
    const openMilestone = milestones.find((milestone) =>
      !["released", "refunded", "exited"].includes(milestone.status),
    );
    const amountSats = openMilestone?.amount_sats || deal?.amount_sats || 0;
    if (!payeeWalletURL.trim() || !payeeWalletKey.trim()) {
      setPayeeWalletError("Enter your LNbits wallet URL and invoice key");
      return;
    }
    if (amountSats <= 0) {
      setPayeeWalletError("No open milestone amount is available for this invoice");
      return;
    }
    setIsGeneratingPayeeInvoice(true);
    setPayeeWalletError(null);
    try {
      const res = await api.generateInvoice(
        {
          wallet_url: payeeWalletURL.trim(),
          wallet_key: payeeWalletKey.trim(),
          amount_sats: amountSats,
          memo: deal ? `Ganji payout: ${deal.title}` : "Ganji payout",
        },
        tokens!.access_token,
      );
      setPayeeInvoiceInput(res.invoice.bolt11);
      setShowPayeeWalletDialog(false);
      setPayeeWalletKey("");
      setNotice(`Generated ${formatCurrency(amountSats)} invoice. Click Save Invoice to use it.`);
    } catch (err) {
      setPayeeWalletError(
        err instanceof Error ? err.message : "Failed to generate invoice",
      );
    } finally {
      setIsGeneratingPayeeInvoice(false);
    }
  }

  async function handleCopyShareLink() {
    if (!deal) return;
    const url = `${window.location.origin}/public/deals/${deal.share_token}`;
    try {
      await copyText(url);
      setNotice("Share link copied to clipboard");
      setTimeout(() => setNotice(null), 3000);
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : "Could not copy share link",
      );
    }
  }

  async function handleExitDeal() {
    if (
      !window.confirm(
        "Exit this deal? Mid-milestone exit (not submitted/approved) cancels that milestone hold per redesign.",
      )
    )
      return;
    await handleAction("exit deal", async () => {
      await api.exitDeal(dealId, tokens!.access_token);
      await fetchDeal(true);
      await fetchMilestones();
    });
  }

  async function handleRefreshEscrowStatus() {
    await handleAction("refresh escrow status", async () => {
      await fetchDeal(true);
      await fetchMilestones();
      await fetchArtifacts();
    });
  }

  async function handleCheckPayment() {
    if (!tokens) return;
    await handleAction("check payment", async () => {
      const res = await api.checkPayment(dealId, tokens.access_token);
      setDeal(res.deal);
      // Show banner and change button after funding is confirmed
      if (res.deal.status === "locked") {
        setShowBanner(true);
      }
      await fetchMilestones();
      await fetchArtifacts();
    });
  }

  async function handleRefreshStatus() {
    if (!tokens) return;
    setActionError(null);
    setActionLoading("refresh status");
    try {
      await Promise.all([fetchDeal(true), fetchMilestones(), fetchArtifacts()]);
    } catch (err) {
      setActionError(
        err instanceof Error
          ? err.message
          : "Failed to refresh freelancer status",
      );
    } finally {
      setActionLoading(null);
    }
  }

  async function handleRejectWork() {
    if (!tokens || !deal) return;
    const milestone = milestones.find((m) =>
      ["work_submitted", "completed"].includes(m.status),
    );
    if (!milestone) {
      setActionError("No submitted milestone is available to reject.");
      return;
    }
    const reason = prompt("Reason for rejecting this work (required):");
    if (!reason?.trim()) return;
    await handleAction("reject work", async () => {
      await api.publicRejectMilestone(
        deal.share_token,
        milestone.id,
        reason.trim(),
        tokens.access_token,
      );
    });
  }

  async function handleResetPayout() {
    if (!tokens || !deal) return;
    const milestoneId = milestones.find((m) => m.payout_reset_available)?.id;
    if (!deal.payout_reset_available && !milestoneId) return;
    if (
      !window.confirm(
        "Clear the failed payout attempt? This only removes the internal attempt marker. It does not move funds or change the milestone status.",
      )
    )
      return;
    await handleAction("clear failed payout", async () => {
      await api.publicResetPayout(
        deal.share_token,
        milestoneId,
        tokens.access_token,
      );
    });
  }

  if (isLoading) {
    return (
      <main className="flex min-h-screen flex-col bg-vault-bg">
        <DealHeader user={user} role={roleHint ?? undefined} />
        <div className="flex-1 flex items-center justify-center px-6">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
        </div>
      </main>
    );
  }

  if (!deal || loadError) {
    return (
      <main className="flex min-h-screen flex-col bg-vault-bg">
        <DealHeader user={user} role={roleHint ?? undefined} />
        <div className="flex-1 flex items-center justify-center px-6">
          <div className="text-center">
            <p className="text-ink-500 mb-4">{loadError || "Deal not found"}</p>
            <Link href="/dashboard">
              <Button variant="ghost">Back to Dashboard</Button>
            </Link>
          </div>
        </div>
      </main>
    );
  }

  const canViewAsFreelancer = user?.id === deal.freelancer_id;
  const canViewAsClient =
    !!user && user.email.toLowerCase() === deal.client_email.toLowerCase();
  const viewerRole: "client" | "freelancer" | "operator" = user?.is_operator
    ? "operator"
    : roleHint === "client" && canViewAsClient
      ? "client"
      : roleHint === "freelancer" && canViewAsFreelancer
        ? "freelancer"
        : canViewAsClient && !canViewAsFreelancer
          ? "client"
          : "freelancer";
  const isFreelancer = viewerRole === "freelancer" && canViewAsFreelancer;
  const isClient = viewerRole === "client" && canViewAsClient;
  const canAttachArtifact = isFreelancer && deal.status === "locked";
  const canSubmit = canAttachArtifact && artifacts.length > 0;
  const fundsHeld = ["locked", "work_submitted", "reviewing"].includes(
    deal.status,
  );
  const escrowMessage =
    deal.status === "locked"
      ? "Funds held in Lightning escrow. Ganji verified the hold; work can proceed."
      : deal.status === "work_submitted"
        ? "Work submitted. Funds remain held in escrow while the client reviews and approves payout."
        : "Work under review. Funds remain held in escrow while the client decides.";
  const canRotateLink = isFreelancer;
  const canUpdatePayeeInvoice =
    isFreelancer && deal.status !== "released" && deal.status !== "refunded";
  const currentPayeeInvoice =
    milestones.find((milestone) =>
      !["released", "refunded", "exited"].includes(milestone.status),
    )?.payee_invoice || deal.payee_invoice || "";
  const clientCanApprove = ["work_submitted", "reviewing", "disputed"].includes(
    deal.status,
  );
  const clientCanReject = milestones.some((milestone) =>
    ["work_submitted", "completed"].includes(milestone.status),
  );
  const clientCanResetPayout =
    deal.payout_reset_available ||
    milestones.some((milestone) => milestone.payout_reset_available);

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <DealHeader user={user} role={viewerRole} />

      <div className="flex-1 px-6 py-8 md:py-12">
        <div className="mx-auto max-w-4xl">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-8">
            <div>
              <Link
                href={viewerRole === "client" ? "/dashboard?role=client" : viewerRole === "freelancer" ? "/dashboard?role=freelancer" : "/dashboard"}
                className="inline-flex items-center gap-2 text-sm text-ink-500 hover:text-ink-300 mb-2 block"
              >
                ← Dashboard
              </Link>
              <h1 className="font-display text-3xl font-semibold text-ink-100">
                {deal.title}
              </h1>
            </div>
            <span
              className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium border ${getStatusClass(deal.status)}`}
            >
              <StatusIcon status={deal.status} /> {getStatusLabel(deal.status)}
            </span>
          </div>

          {(actionError || notice) && (
            <div
              className={`mb-6 p-4 rounded-lg text-sm border ${actionError ? "bg-yellow-600/10 border-yellow-600/30 text-yellow-400" : "bg-green-600/10 border-green-600/30 text-green-400"}`}
            >
              {actionError || notice}
            </div>
          )}

          {fundsHeld && (
            <div className="mb-6 p-4 rounded-lg bg-blue-600/10 border border-blue-600/30 text-sm text-blue-400">
              <span className="inline-flex items-center gap-1.5">
                <StatusIcon status={deal.status} /> {escrowMessage}
              </span>
            </div>
          )}

          <div className="grid lg:grid-cols-3 gap-6">
            <div className="lg:col-span-2 space-y-6">
              {user?.is_operator && deal && (
                <div className="vault-card p-6 border-gold-500/30 bg-gold-500/5">
                  <div className="flex items-center justify-between gap-3 mb-3">
                    <h2 className="font-display text-lg font-semibold text-ink-100">Operator Actions</h2>
                    <span className="text-xs px-2 py-1 rounded-full border border-gold-500/30 text-gold-400">Operator view</span>
                  </div>
                  <p className="text-sm text-ink-400 mb-4">
                    This is a read-only triage view. Use reconcile for stuck payout attempts; do not resend payments from ambiguous states.
                  </p>
                  <div className="flex flex-wrap gap-2">
                    <Link href={`/operator/deals/${deal.id}/reconcile`}>
                      <Button variant="outline" size="sm">Open Reconcile</Button>
                    </Link>
                    <Link href="/operator/disputes">
                      <Button variant="ghost" size="sm">Disputes Queue</Button>
                    </Link>
                  </div>
                  {(deal.payout_reset_available || milestones.some((m) => m.payout_reset_available)) && (
                    <p className="mt-3 text-xs text-red-400">Stuck payout attempt detected — reconcile required before retry.</p>
                  )}
                </div>
              )}

              <DealCard deal={deal} viewerRole={viewerRole} />

              <div className="vault-card p-6 vault-seam">
                <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">
                  Deal Details
                </h2>
                <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
                  <div>
                    <dt className="text-ink-500">Amount</dt>
                    <dd className="font-mono text-lg font-medium text-ink-100 mono-tabular">
                      {formatCurrency(deal.amount_sats)}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Source Platform</dt>
                    <dd className="text-ink-300 font-mono">
                      {deal.source_platform}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Client</dt>
                    <dd className="text-ink-300 font-mono truncate">
                      {deal.client_email}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Freelancer</dt>
                    <dd className="text-ink-300 font-mono">
                      {deal.freelancer_id === user?.id
                        ? "You"
                        : deal.freelancer_id}
                    </dd>
                  </div>
                  <div className="sm:col-span-2">
                    <dt className="text-ink-500">Preimage Hash</dt>
                    <dd className="font-mono text-xs text-ink-400 break-all">
                      {deal.preimage_hash}
                    </dd>
                  </div>
                  <div className="sm:col-span-2">
                    <dt className="text-ink-500">LNbits Checking ID</dt>
                    <dd className="font-mono text-xs text-ink-400">
                      {deal.checking_id || "—"}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Created</dt>
                    <dd className="text-ink-300 font-mono">
                      {new Date(deal.created_at).toLocaleString()}
                    </dd>
                  </div>
                  {deal.verified_at && (
                    <div>
                      <dt className="text-ink-500">Anchored to Bitcoin</dt>
                      <dd className="text-ink-300 font-mono text-green-400">
                        {new Date(deal.verified_at).toLocaleString()}
                      </dd>
                    </div>
                  )}
                </dl>
              </div>

              {deal.dispute_reason && (
                <div className="vault-card p-6 vault-seam border-red-600/30 bg-red-600/5">
                  <h2 className="font-display text-lg font-semibold text-red-400 mb-2 flex items-center gap-2">
                    <StatusIcon status="disputed" /> Dispute
                  </h2>
                  <p className="text-ink-300">{deal.dispute_reason}</p>
                  {deal.disputed_at && (
                    <p className="mt-2 text-xs text-ink-500 font-mono">
                      Disputed at {new Date(deal.disputed_at).toLocaleString()}
                    </p>
                  )}
                </div>
              )}

              {(isFreelancer || isClient || artifacts.length > 0) && (
                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">
                    Deliverables
                  </h2>
                  {canAttachArtifact && (
                    <div className="space-y-4 mb-6">
                      <div className="space-y-2">
                        <label
                          htmlFor="work-link"
                          className="text-sm font-medium text-ink-300"
                        >
                          Work link or delivery note
                        </label>
                        <textarea
                          id="work-link"
                          rows={3}
                          value={workLink}
                          onChange={(event) => setWorkLink(event.target.value)}
                          placeholder="Paste a link to the delivered work, repository, or document"
                          className="w-full rounded-lg border border-vault-border bg-vault-raised px-3 py-2.5 text-sm text-ink-100 placeholder:text-ink-500 focus:outline-none focus:ring-2 focus:ring-gold-500 focus:border-transparent"
                        />
                        <Button
                          size="sm"
                          onClick={handleAttachWorkLink}
                          disabled={
                            !workLink.trim() ||
                            actionLoading === "attach work link"
                          }
                          isLoading={actionLoading === "attach work link"}
                        >
                          Attach Work Link
                        </Button>
                      </div>
                      <div className="space-y-2">
                        <label
                          htmlFor="artifact-file"
                          className="text-sm font-medium text-ink-300"
                        >
                          Upload a deliverable file
                        </label>
                        <div className="flex flex-wrap gap-2">
                          <input
                            ref={fileInputRef}
                            id="artifact-file"
                            type="file"
                            onChange={(event) =>
                              setSelectedFile(event.target.files?.[0] ?? null)
                            }
                            className="flex-1 min-w-[200px] rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm text-ink-300 file:mr-3 file:rounded file:border-0 file:bg-gold-500 file:px-3 file:py-1 file:text-vault-bg"
                          />
                          <select
                            aria-label="Artifact kind"
                            value={artifactKind}
                            onChange={(event) =>
                              setArtifactKind(
                                event.target.value as ArtifactKind,
                              )
                            }
                            className="rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm text-ink-300"
                          >
                            <option value="source_file">source_file</option>
                            <option value="source_code">source_code</option>
                          </select>
                          <Button
                            size="sm"
                            onClick={handleUploadFile}
                            disabled={
                              !selectedFile ||
                              actionLoading === "upload artifact"
                            }
                            isLoading={actionLoading === "upload artifact"}
                          >
                            Upload File
                          </Button>
                        </div>
                      </div>
                    </div>
                  )}
                  {artifacts.length === 0 ? (
                    <p className="text-sm text-ink-500">
                      No deliverables attached yet.
                    </p>
                  ) : (
                    <ul className="space-y-2">
                      {artifacts.map((artifact) => (
                        <li
                          key={artifact.id}
                          className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-vault-border bg-vault-raised px-3 py-2"
                        >
                          <div className="min-w-0">
                            <p className="truncate font-mono text-sm text-ink-200">
                              {artifact.storage_key.split("/").pop()}
                            </p>
                            <p className="text-xs text-ink-500">
                              {artifact.kind} •{" "}
                              {new Date(artifact.uploaded_at).toLocaleString()}
                            </p>
                          </div>
                          <div className="flex gap-2">
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleDownloadArtifact(artifact)}
                              disabled={actionLoading === "download artifact"}
                              isLoading={actionLoading === "download artifact"}
                            >
                              Download
                            </Button>
                            {canAttachArtifact && (
                              <Button
                                variant="danger"
                                size="sm"
                                onClick={() =>
                                  handleDeleteArtifact(artifact.id)
                                }
                                disabled={actionLoading === "delete artifact"}
                                isLoading={actionLoading === "delete artifact"}
                              >
                                Delete
                              </Button>
                            )}
                          </div>
                        </li>
                      ))}
                    </ul>
                  )}
                  {canAttachArtifact && artifacts.length === 0 && (
                    <p className="mt-3 text-xs text-yellow-400">
                      Attach at least one deliverable before clicking Submit
                      Work.
                    </p>
                  )}
                </div>
              )}

              {isFreelancer && (
                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">
                    Actions
                  </h2>
                  <div className="flex flex-wrap gap-3">
                    {canAttachArtifact && !canSubmit && (
                      <span className="flex items-center px-3 py-2 text-sm text-ink-500">
                        Submit Work unlocks after a deliverable is attached.
                      </span>
                    )}
                    {canSubmit && (
                      <Button
                        onClick={handleSubmitWork}
                        disabled={actionLoading === "submit work"}
                        isLoading={actionLoading === "submit work"}
                      >
                        Submit Work
                      </Button>
                    )}
                    {canRotateLink && (
                      <Button
                        variant="ghost"
                        onClick={handleRotateShareLink}
                        disabled={actionLoading === "rotate share link"}
                        isLoading={actionLoading === "rotate share link"}
                      >
                        Rotate Share Link
                      </Button>
                    )}
                    {canUpdatePayeeInvoice && (
                      <Button
                        variant="outline"
                        onClick={() => {
                          setPayeeInvoiceInput("");
                          setShowPayeeInvoiceForm((open) => !open);
                          setActionError(null);
                          setNotice(null);
                        }}
                        disabled={!!actionLoading}
                      >
                        Update Payee Invoice
                      </Button>
                    )}
                    {deal.status === "awaiting_payment" && (
                      <Button
                        variant="outline"
                        onClick={handleCheckPayment}
                        disabled={actionLoading === "check payment"}
                        isLoading={actionLoading === "check payment"}
                      >
                        Check Payment
                      </Button>
                    )}
                    {deal.status === "locked" && (
                      <Button
                        variant="outline"
                        onClick={() => setShowBanner(true)}
                        disabled={actionLoading}
                        isLoading={actionLoading}
                      >
                        Confirm Funded
                      </Button>
                    )}
                    {deal.status === "locked" && (
                      <Button
                        variant="ghost"
                        onClick={handleRefreshEscrowStatus}
                        disabled={actionLoading === "refresh escrow status"}
                        isLoading={actionLoading === "refresh escrow status"}
                      >
                        Check Escrow Status
                      </Button>
                    )}
                    {(deal.status === "work_submitted" ||
                      deal.status === "reviewing") && (
                      <span className="flex items-center px-3 py-2 text-sm text-blue-300">
                        {deal.status === "work_submitted"
                          ? "Waiting for client approval."
                          : "Client review in progress."}
                      </span>
                    )}
                    {(deal.status === "work_submitted" ||
                      deal.status === "released") && (
                      <>
                        <Button
                          variant="ghost"
                          onClick={handleRefreshStatus}
                          disabled={actionLoading === "refresh status"}
                          isLoading={actionLoading === "refresh status"}
                        >
                          Check Freelancer Status
                        </Button>
                        {deal.payout_reset_available && (
                          <span className="flex items-center px-3 py-2 text-sm text-yellow-400">
                            Payout needs operator confirmation before retry.
                          </span>
                        )}
                      </>
                    )}
                    {deal.status !== "released" &&
                      deal.status !== "refunded" && (
                        <Button
                          variant="outline"
                          onClick={handleExitDeal}
                          disabled={actionLoading === "exit deal"}
                          isLoading={actionLoading === "exit deal"}
                        >
                          Exit Deal
                        </Button>
                      )}
                    {deal.status === "released" && (
                      <span className="flex items-center px-3 py-2 text-sm text-green-400 bg-green-600/10 rounded-lg border border-green-600/30">
                        <StatusIcon status="released" /> Released —{" "}
                        {formatCurrency(deal.amount_sats)} paid to your invoice
                      </span>
                    )}
                  </div>
                  {showPayeeInvoiceForm && canUpdatePayeeInvoice && (
                    <div className="mt-5 rounded-lg border border-vault-border bg-vault-raised p-4">
                      <h3 className="font-medium text-ink-100 mb-2">Replace payout invoice</h3>
                      <p className="text-xs text-ink-400 mb-3">
                        Use this if a payout invoice expired. Start by pasting an invoice, use Retry Current Invoice to load the stored one, or use Generate Invoice to create a fresh one from LNbits. The client is not charged again; already-held escrow funds are paid to this invoice when approved/reconciled.
                      </p>
                      <textarea
                        value={payeeInvoiceInput}
                        onChange={(e) => setPayeeInvoiceInput(e.target.value)}
                        rows={3}
                        placeholder="lnbc..."
                        className="w-full rounded-lg border border-vault-border bg-vault-bg px-3 py-2 text-xs font-mono text-ink-200"
                      />
                      <div className="mt-3 flex flex-wrap gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setPayeeInvoiceInput(currentPayeeInvoice);
                            setActionError(null);
                            setNotice(
                              currentPayeeInvoice
                                ? "Stored invoice loaded. Save it to repair the payout target."
                                : "No stored invoice is available.",
                            );
                          }}
                          disabled={!currentPayeeInvoice || !!actionLoading}
                        >
                          Retry Current Invoice
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setPayeeInvoiceInput("");
                            setPayeeWalletError(null);
                            setNotice(null);
                            setShowPayeeWalletDialog(true);
                          }}
                          disabled={!!actionLoading || isGeneratingPayeeInvoice}
                        >
                          Generate Invoice
                        </Button>
                        <Button
                          size="sm"
                          onClick={handleUpdatePayeeInvoice}
                          disabled={actionLoading === "update payee invoice" || !payeeInvoiceInput.trim()}
                          isLoading={actionLoading === "update payee invoice"}
                        >
                          Save Invoice
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => {
                            setShowPayeeInvoiceForm(false);
                            setPayeeInvoiceInput("");
                          }}
                          disabled={!!actionLoading}
                        >
                          Cancel
                        </Button>
                      </div>
                    </div>
                  )}
                  {deal.status !== "released" && deal.status !== "refunded" && (
                    <p className="text-xs text-ink-400 mt-3">
                      Exit mid-milestone (not submitted/approved): that
                      milestone’s escrow hold is cancelled/refunded per
                      redesign.
                    </p>
                  )}
                </div>
              )}

              <WalletLinkDialog
                open={showPayeeWalletDialog}
                title="Generate payout invoice"
                walletURL={payeeWalletURL}
                walletKey={payeeWalletKey}
                keyLabel="Wallet invoice key"
                keyPlaceholder="LNbits invoice key"
                submitLabel="Generate invoice"
                error={payeeWalletError}
                isSubmitting={isGeneratingPayeeInvoice}
                onClose={() => {
                  setShowPayeeWalletDialog(false);
                  setPayeeWalletError(null);
                }}
                onWalletURLChange={setPayeeWalletURL}
                onWalletKeyChange={setPayeeWalletKey}
                onSubmit={handleGeneratePayeeInvoice}
              />

              {showBanner && (
                <div className="vault-card p-4 mt-4">
                  <p className="text-ink-400">{escrowMessage}</p>
                </div>
              )}

              {isClient && (
                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">
                    Client Actions
                  </h2>
                  {(clientCanApprove || clientCanReject) && (
                    <div className="mb-4 p-4 rounded-lg bg-gold-500/5 border border-gold-500/30">
                      <p className="font-medium text-gold-400 mb-1">
                        Review the delivered work
                      </p>
                      <p className="text-sm text-ink-300">
                        Approving releases the already-held escrow to the
                        freelancer; you are not charged again. Rejecting pauses
                        payout for operator review; it does not refund you.
                      </p>
                    </div>
                  )}
                  <div className="flex flex-wrap gap-3">
                    {clientCanApprove && (
                      <Button
                        onClick={() =>
                          handleAction("approve", async () => {
                            await api.publicApproveDeal(
                              deal.share_token,
                              tokens!.access_token,
                            );
                          })
                        }
                        disabled={actionLoading === "approve"}
                        isLoading={actionLoading === "approve"}
                        size="lg"
                      >
                        Approve & Release
                      </Button>
                    )}
                    {clientCanReject && (
                      <Button
                        variant="outline"
                        onClick={handleRejectWork}
                        disabled={actionLoading === "reject work"}
                        isLoading={actionLoading === "reject work"}
                        size="lg"
                      >
                        Reject Submitted Work
                      </Button>
                    )}
                    {clientCanResetPayout && (
                      <Button
                        variant="outline"
                        onClick={handleResetPayout}
                        disabled={actionLoading === "clear failed payout"}
                        isLoading={actionLoading === "clear failed payout"}
                      >
                        Clear Failed Payout
                      </Button>
                    )}
                    {deal.status === "released" && (
                      <span className="flex items-center px-3 py-2 text-sm text-green-400 bg-green-600/10 rounded-lg border border-green-600/30">
                        <StatusIcon status="released" /> Payment confirmed to
                        the freelancer
                      </span>
                    )}
                    {deal.status === "disputed" && (
                      <span className="flex items-center px-3 py-2 text-sm text-yellow-400 bg-yellow-600/10 rounded-lg border border-yellow-600/30">
                        <StatusIcon status="disputed" /> Submitted work rejected
                        — held for operator review
                      </span>
                    )}
                    {deal.status === "refunded" && (
                      <span className="flex items-center px-3 py-2 text-sm text-gray-400 bg-gray-600/10 rounded-lg border border-gray-600/30">
                        <StatusIcon status="refunded" /> Escrow refunded
                      </span>
                    )}
                    {deal.status === "awaiting_payment" && (
                      <Button
                        variant="ghost"
                        onClick={handleRefreshEscrowStatus}
                        disabled={actionLoading === "refresh escrow status"}
                        isLoading={actionLoading === "refresh escrow status"}
                      >
                        Check Escrow Status
                      </Button>
                    )}
                    {deal.status !== "released" &&
                      deal.status !== "refunded" && (
                        <Button
                          variant="danger"
                          onClick={handleExitDeal}
                          disabled={actionLoading === "exit deal"}
                          isLoading={actionLoading === "exit deal"}
                        >
                          Exit Deal
                        </Button>
                      )}
                  </div>
                </div>
              )}

              <div className="space-y-6">
                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">
                    Milestones
                  </h2>
                  {milestones.length === 0 ? (
                    <div className="p-4 rounded-lg border border-vault-border bg-vault-raised/40">
                      <div className="flex justify-between items-start gap-3">
                        <div>
                          <h3 className="font-medium text-ink-100">
                            #1 Final delivery
                          </h3>
                          <p className="text-sm text-ink-500">
                            Sats: {formatCurrency(deal.amount_sats)} •
                            Single-step deal
                          </p>
                          <p className="mt-1 text-xs text-gold-400">
                            Work submitted on the deal is recorded here.
                          </p>
                        </div>
                        <span
                          className={`inline-flex items-center gap-1.5 px-2 py-1 rounded text-xs font-medium border ${getStatusClass(deal.status)}`}
                        >
                          <StatusIcon status={deal.status} />{" "}
                          {getStatusLabel(deal.status)}
                        </span>
                      </div>
                    </div>
                  ) : (
                    <div className="space-y-4">
                      {milestones.map((ms) => (
                        <div
                          key={ms.id}
                          className="p-4 rounded-lg border transition-colors focus-within:outline-none focus-within:ring-2 focus-within:ring-gold-500"
                        >
                          <div className="flex justify-between items-start">
                            <div>
                              <h3 className="font-medium text-ink-100">
                                {ms.title}
                              </h3>
                              <p className="text-sm text-ink-500">
                                Sats: {formatCurrency(ms.amount_sats)} • Order:
                                #{ms.order}
                              </p>
                            </div>
                            <span
                              className={`inline-flex items-center gap-1.5 px-2 py-1 rounded text-xs font-medium ${getMilestoneStatusClass(ms.status)}`}
                            >
                              {getMilestoneStatusLabel(ms.status)}
                            </span>
                          </div>
                          {ms.completed_at && (
                            <p className="mt-1 text-xs text-ink-500 font-mono">
                              Completed:{" "}
                              {new Date(ms.completed_at).toLocaleString()}
                            </p>
                          )}
                          <p className="mt-1 text-xs text-ink-500">
                            Delivery estimate: {ms.delivery_days} days from
                            funding
                            {ms.due_at
                              ? ` · Due ${new Date(ms.due_at).toLocaleDateString()}`
                              : ""}
                          </p>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">
                    Timeline
                  </h2>
                  <StatusTimeline deal={deal} />
                </div>

                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">
                    Verification
                  </h2>
                  <p className="text-sm text-ink-500 mb-3">
                    This deal will be anchored to Bitcoin via OpenTimestamps
                    when released.
                  </p>
                  {deal.verified_at ? (
                    <div className="p-3 rounded-lg bg-green-600/10 border border-green-600/30 text-green-400 text-sm">
                      <span className="inline-flex items-center gap-1.5">
                        <StatusIcon status="valid" /> Verified on Bitcoin at{" "}
                        {new Date(deal.verified_at).toLocaleString()}
                      </span>
                    </div>
                  ) : (
                    <div className="p-3 rounded-lg bg-yellow-600/10 border border-yellow-600/30 text-yellow-400 text-sm">
                      <span className="inline-flex items-center gap-1.5">
                        <StatusIcon status="awaiting_payment" /> Pending
                        verification (anchors on release)
                      </span>
                    </div>
                  )}
                </div>
              </div>
            </div>

            <div className="space-y-6">
              <div className="vault-card p-6 vault-seam">
                <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">
                  Share Link
                </h2>
                <p className="text-sm text-ink-500 mb-4">
                  Send this to your client. They can view the deal and approve
                  payment.
                </p>
                <div className="space-y-3">
                  <div className="flex gap-2">
                    <input
                      type="text"
                      readOnly
                      value={`${window.location.origin}/public/deals/${deal.share_token}`}
                      className="flex-1 rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm font-mono text-ink-300"
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={handleCopyShareLink}
                    >
                      Copy
                    </Button>
                  </div>
                  <p className="text-xs text-ink-500">
                    Status:{" "}
                    <span className="font-mono capitalize">
                      {deal.status.replace("_", " ")}
                    </span>
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </main>
  );
}
