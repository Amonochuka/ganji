"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { WalletLinkDialog } from "@/components/deals/wallet-link-dialog";
import { StatusIcon } from "@/components/ui/status-icon";
import { api, type PublicDeal, type Artifact } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";
import { MilestoneRow, getStatusClass, getStatusLabel } from "./deal-ui";
import { formatCurrency } from "@/lib/utils/format";

export default function PublicDealPage() {
  const params = useParams();
  const shareToken = params.shareToken as string;
  const { tokens, isLoading: authLoading } = useAuth();
  const signInHref = `/login?next=${encodeURIComponent(`/public/deals/${shareToken}`)}`;

  const [deal, setDeal] = useState<PublicDeal | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [fundTarget, setFundTarget] = useState<{ milestoneId?: string; label: string } | null>(null);
  const [walletURL, setWalletURL] = useState("");
  const [walletKey, setWalletKey] = useState("");
  const [walletError, setWalletError] = useState<string | null>(null);

  useEffect(() => {
    fetchDeal();
  }, [shareToken]);

  useEffect(() => {
    if (!deal) return;
    if (["released", "refunded", "disputed"].includes(deal.status)) return;
    const interval = setInterval(() => fetchDeal(true), 5000);
    return () => clearInterval(interval);
  }, [deal?.status, shareToken]);

  async function fetchDeal(silent = false) {
    try {
      if (!silent) setIsLoading(true);
      const res = await api.getPublicDeal(shareToken);
      setDeal(res.deal);
      setError(null);
      void fetchArtifacts(silent);
    } catch (err) {
      if (!silent) setError(err instanceof Error ? err.message : "Failed to load deal");
    } finally {
      if (!silent) setIsLoading(false);
    }
  }

  async function fetchArtifacts(silent = true) {
    try {
      const res = await api.listPublicArtifacts(shareToken);
      setArtifacts(res.artifacts);
    } catch (err) {
      if (!silent) {
        setActionError(
          err instanceof Error ? err.message : "Failed to load deliverables",
        );
      }
    }
  }

  function requireClientToken(): string {
    if (!tokens?.access_token) {
      throw new Error("Sign in with the deal's client email to authorize this action.");
    }
    return tokens.access_token;
  }

  async function runAction(key: string, fn: () => Promise<void>) {
    setActionLoading(key);
    setActionError(null);
    try {
      await fn();
      await fetchDeal(true);
      setActionError(null);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : `Failed: ${key}`);
      await fetchDeal(true);
    } finally {
      setActionLoading(null);
    }
  }

  function openFundDialog(milestoneId?: string, label = "this deal") {
    setFundTarget({ milestoneId, label });
    setWalletURL("");
    setWalletKey("");
    setWalletError(null);
    setActionError(null);
  }

  function closeFundDialog() {
    setFundTarget(null);
    setWalletKey("");
    setWalletError(null);
  }

  async function handleFund() {
    if (!fundTarget) return;
    const key = fundTarget.milestoneId ? `fund-${fundTarget.milestoneId}` : "fund";
    setActionLoading(key);
    setActionError(null);
    setWalletError(null);
    try {
      const res = await api.fundPublicDeal(shareToken, {
        wallet_url: walletURL.trim(),
        wallet_key: walletKey.trim(),
        ...(fundTarget.milestoneId ? { milestone_id: fundTarget.milestoneId } : {}),
      });
      setDeal(res.deal);
      closeFundDialog();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Funding failed";
      setActionError(message);
      setWalletError(message);
      await fetchDeal(true);
    } finally {
      setActionLoading(null);
    }
  }

  async function handleApprove() {
    await runAction("approve", async () => {
      const res = await api.publicApproveDeal(shareToken, requireClientToken());
      setDeal(res.deal);
    });
  }

  async function handleCheckFreelancerStatus() {
    setActionError(null);
    setActionLoading("check freelancer status");
    try {
      const res = await api.getPublicDeal(shareToken);
      setDeal(res.deal);
      setError(null);
      void fetchArtifacts(true);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Failed to refresh deal status");
    } finally {
      setActionLoading(null);
    }
  }

  async function handleResetPayout(milestoneId?: string) {
    const message = milestoneId
      ? "Clear the failed payout attempt for this milestone? This only removes the internal attempt marker. It does not move funds, settle escrow, or change the milestone status. Continue only after confirming the freelancer was not paid."
      : "Clear the failed payout attempt for this deal? This only removes the internal attempt marker. It does not move funds, settle escrow, or change the deal status. Continue only after confirming the freelancer was not paid.";
    if (!window.confirm(message)) return;
    await runAction(milestoneId ? `reset-${milestoneId}` : "reset", async () => {
      const res = await api.publicResetPayout(shareToken, milestoneId, requireClientToken());
      setDeal(res.deal);
    });
  }

  async function handleAcceptNext(milestoneId: string) {
    await runAction(`accept-${milestoneId}`, async () => {
      await api.publicAcceptNextMilestone(shareToken, milestoneId, requireClientToken());
    });
  }

  async function handleReject(milestoneId: string) {
    const reason = prompt("Reason for rejection (required):");
    if (!reason?.trim()) return;
    await runAction(`reject-${milestoneId}`, async () => {
      await api.publicRejectMilestone(shareToken, milestoneId, reason.trim(), requireClientToken());
    });
  }

  async function handleExit() {
    const hasMilestones = (deal?.milestones || []).length > 0;
    const message = hasMilestones
      ? "Exit this deal? If the current milestone has not been submitted/approved, its escrow for that milestone will not be released to the freelancer (refunded/cancelled per policy)."
      : "Exit this deal? The held escrow will be cancelled or refunded, and no payout will be sent to the freelancer.";
    if (!window.confirm(message)) return;
    await runAction("exit", async () => {
      await api.publicExitDeal(shareToken, requireClientToken());
    });
  }

  async function handleDownloadArtifact(artifact: Artifact) {
    setActionLoading(`download-${artifact.id}`);
    setActionError(null);
    try {
      const blob = await api.downloadPublicArtifact(shareToken, artifact.id);
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = artifact.storage_key.split("/").pop() || "artifact";
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Failed to download artifact");
    } finally {
      setActionLoading(null);
    }
  }

  if (isLoading) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center bg-vault-bg px-6">
        <div className="w-full max-w-xl text-center">
          <Link href="/" className="logo-gnj inline-block mb-8" aria-label="Ganji Home">ganji</Link>
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent mx-auto" />
          <p className="mt-4 text-ink-500">Loading deal...</p>
        </div>
      </main>
    );
  }

  if (error || !deal) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center bg-vault-bg px-6">
        <div className="w-full max-w-xl text-center">
          <Link href="/" className="logo-gnj inline-block mb-8" aria-label="Ganji Home">ganji</Link>
          <div className="vault-card p-8 vault-seam">
            <StatusIcon status="search" className="mx-auto mb-4 h-10 w-10 text-ink-500" />
            <h1 className="font-display text-2xl font-semibold text-ink-100 mb-2">Deal Not Found</h1>
            <p className="text-ink-500 mb-6">{error || "This deal link is invalid or has expired."}</p>
            <div className="flex items-center justify-center gap-3">
              <Button variant="ghost" onClick={() => { setError(null); fetchDeal(); }}>Try Again</Button>
              <Link href="/"><Button variant="ghost">Go to Ganji</Button></Link>
            </div>
          </div>
        </div>
      </main>
    );
  }

  const isPaid = deal.status === "released";
  const isRefunded = deal.status === "refunded";
  const isDisputed = deal.status === "disputed";
  const canApproveDeal = deal.status === "work_submitted" || deal.status === "reviewing";
  const fundsHeld = ["locked", "work_submitted", "reviewing"].includes(deal.status);
  const canResetDealPayout = !!deal.payout_reset_available;
  const milestones = deal.milestones || [];
  const hasMilestones = milestones.length > 0;
  const terminal = isPaid || isRefunded;

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-3xl items-center justify-between px-6">
          <Link href="/" className="brand-wordmark" aria-label="Ganji Home">ganji</Link>
          <span className="text-xs text-ink-500">Client review (share link)</span>
        </div>
      </header>

      <div className="flex-1 px-6 py-10">
        <div className="mx-auto w-full max-w-xl space-y-6">
          <div className="text-center">
            <span className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium border ${getStatusClass(deal.status)}`}>
              <StatusIcon status={deal.status} /> {getStatusLabel(deal.status)}
            </span>
          </div>

          <div className="vault-card p-6 md:p-8 vault-seam">
            <h1 className="font-display text-2xl md:text-3xl font-semibold text-ink-100 mb-2">{deal.title}</h1>
            <p className="text-ink-500 mb-6 font-mono">{deal.source_platform}</p>

            <div className="mb-6 p-4 rounded-lg bg-vault-raised border border-vault-border">
              <p className="text-xs text-ink-500 uppercase tracking-wide mb-1">Total Amount</p>
              <p className="font-mono text-3xl font-medium text-ink-100 mono-tabular">{formatCurrency(deal.amount_sats)}</p>
            </div>

            {deal.dispute_reason && (
              <div className="mb-6 p-4 rounded-lg bg-red-600/10 border border-red-600/30">
                <p className="flex items-center justify-center gap-1.5 font-medium text-red-400 mb-1"><StatusIcon status="disputed" /> Dispute / Issue</p>
                <p className="text-sm text-ink-300">{deal.dispute_reason}</p>
              </div>
            )}

            {isPaid && (
              <div className="mb-6 p-4 rounded-lg bg-green-600/10 border border-green-600/30 text-center">
                <p className="flex items-center justify-center gap-1.5 font-medium text-green-400 mb-1"><StatusIcon status="released" /> Payment Released</p>
                <p className="text-sm text-ink-300">Payment confirmed to the freelancer.</p>
              </div>
            )}
            {isRefunded && (
              <div className="mb-6 p-4 rounded-lg bg-gray-600/10 border border-gray-600/30 text-center">
                <p className="flex items-center justify-center gap-1.5 font-medium text-gray-400 mb-1"><StatusIcon status="refunded" /> Refunded / Cancelled</p>
                <p className="text-sm text-ink-300">Funds returned per policy for the exited/unfunded portion.</p>
              </div>
            )}
            {fundsHeld && (
              <div className="mb-6 p-4 rounded-lg bg-blue-600/10 border border-blue-600/30 text-center">
                <p className="flex items-center justify-center gap-1.5 font-medium text-blue-400 mb-1"><StatusIcon status="locked" /> Funds held in escrow</p>
                <p className="text-sm text-ink-300">Ganji verified the Lightning hold. The freelancer can see that funding is committed.</p>
                <p className="mt-1 text-xs text-ink-400">The hold is accepted but not settled, so the escrow wallet balance will not increase until payout.</p>
              </div>
            )}

            {actionError && <p className="mb-4 text-sm text-red-400">{actionError}</p>}

            {!authLoading && !tokens && !terminal && (
              <div className="mb-4 rounded-lg border border-yellow-600/30 bg-yellow-600/10 p-3 text-sm text-yellow-300">
                Viewing and funding are public. Approving, rejecting, resetting, or exiting requires signing in as the deal&apos;s client.{" "}
                <Link href={signInHref} className="font-medium underline underline-offset-2">Sign in</Link>
              </div>
            )}

            {deal.payout_reset_available && !hasMilestones && (
              <p className="mb-4 rounded-lg border border-yellow-600/30 bg-yellow-600/10 p-3 text-sm text-yellow-300">
                Payout outcome needs confirmation. Checking account status refreshes the deal; it will not resend payment.
              </p>
            )}

            {!terminal && !isDisputed && (
              <div className="space-y-4">
                <div className="p-4 rounded-lg bg-vault-raised border border-vault-border">
                  <p className="text-xs text-ink-500 uppercase tracking-wide mb-2">Lightning Invoice (current escrow)</p>
                  <div className="flex gap-2">
                    <input type="text" readOnly value={deal.invoice} className="flex-1 rounded-lg border border-vault-border bg-vault-bg px-3 py-2 text-xs font-mono text-ink-400" />
                    <Button variant="ghost" size="sm" onClick={() => navigator.clipboard.writeText(deal.invoice)}>Copy</Button>
                  </div>
                  {!hasMilestones && deal.status === "awaiting_payment" && (
                    <Button className="mt-3 w-full" onClick={() => openFundDialog(undefined, "this deal")} isLoading={actionLoading === "fund"} disabled={!!actionLoading}>
                      Fund from linked wallet
                    </Button>
                  )}
                  {hasMilestones ? (
                    <p className="mt-2 text-xs text-ink-500">
                      Milestone invoices are applied sequentially (M1 → M2…). Each milestone is funded, reviewed, and released before the next begins.
                    </p>
                  ) : (
                    <p className="mt-2 text-xs text-ink-500">
                      Single-step deal: this invoice holds the full amount. Approve &amp; Release Payment settles it to the freelancer.
                    </p>
                  )}
                </div>

                {canResetDealPayout && !hasMilestones && (
                  <div className="p-4 rounded-lg bg-yellow-600/10 border border-yellow-600/30">
                    <p className="font-medium text-yellow-400 mb-1">Payout needs a retry</p>
                    <p className="text-xs text-ink-300 mb-3">
                      A payout attempt was recorded but no payment was confirmed. Clear it before approving again. This does not move funds or change the deal status.
                    </p>
                    <Button
                      variant="outline"
                      className="w-full"
                      onClick={() => handleResetPayout()}
                      isLoading={actionLoading === "reset"}
                      disabled={!!actionLoading}
                    >
                      Clear failed payout attempt
                    </Button>
                  </div>
                )}

                {canApproveDeal && !hasMilestones && !canResetDealPayout && (
                  <Button size="lg" className="w-full py-3.5" onClick={handleApprove} isLoading={actionLoading === "approve"}>
                    Approve & Release Payment
                  </Button>
                )}
              </div>
            )}

            {isDisputed && (
              <p className="text-xs text-ink-500 text-center">This deal is under dispute. An operator will resolve it.</p>
            )}
          </div>

          <div className="vault-card p-6 vault-seam">
            <h2 className="font-display text-lg font-semibold text-ink-100 mb-1">Milestones</h2>
            <p className="text-xs text-ink-500 mb-4">
              {hasMilestones
                ? "Review step-by-step. After approving M1, you can approve/start M2 (sequential per-milestone funding)."
                : "This single-step deal is shown as one final milestone."}
            </p>
            {hasMilestones ? (
              <div className="space-y-3">
                {milestones.map((ms, i) => (
                  <MilestoneRow
                    key={ms.id}
                    ms={ms}
                    isLast={i === milestones.length - 1}
                    canAct={!terminal && !isDisputed}
                    actionLoading={actionLoading}
                    onFund={() => openFundDialog(ms.id, `milestone ${ms.order}`)}
                    onAcceptNext={() => handleAcceptNext(ms.id)}
                    onReject={() => handleReject(ms.id)}
                    onResetPayout={() => handleResetPayout(ms.id)}
                  />
                ))}
              </div>
            ) : (
              <div className="p-4 rounded-lg border border-vault-border bg-vault-raised/40">
                <div className="flex justify-between items-start gap-3">
                  <div>
                    <h3 className="font-medium text-ink-100">#1 Final delivery</h3>
                    <p className="text-sm text-ink-500">{formatCurrency(deal.amount_sats)}</p>
                    <p className="mt-1 text-xs text-gold-400">Work submitted on the deal is recorded here.</p>
                  </div>
                  <span className={`inline-flex items-center px-2 py-1 rounded text-xs font-medium border ${getStatusClass(deal.status)}`}>
                    <StatusIcon status={deal.status} /> {getStatusLabel(deal.status)}
                  </span>
                </div>
              </div>
            )}
            {!terminal && (
              <div className="mt-5 pt-4 border-t border-vault-border">
                <Button variant="danger" className="w-full" onClick={handleExit} isLoading={actionLoading === "exit"} disabled={!!actionLoading}>
                  Exit Deal
                </Button>
                <p className="mt-2 text-xs text-ink-500">
                  {hasMilestones
                    ? "Exit mid-milestone (work not submitted/approved): that milestone is not paid to the freelancer; funds refunded/cancelled per policy. Same symmetry applies to freelancer exits."
                    : "Exit before approval: this single-step deal is not paid to the freelancer; funds are refunded/cancelled per policy. Same symmetry applies to freelancer exits."}
                </p>
              </div>
            )}
          </div>

          <div className="vault-card p-6 vault-seam">
            <h2 className="font-display text-lg font-semibold text-ink-100 mb-1">Deliverables</h2>
            <p className="text-xs text-ink-500 mb-4">Files uploaded by the freelancer for this deal.</p>
            {artifacts.length === 0 ? (
              <p className="text-sm text-ink-500">No deliverables attached yet.</p>
            ) : (
              <ul className="space-y-2">
                {artifacts.map((artifact) => (
                  <li key={artifact.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-vault-border bg-vault-raised px-3 py-2">
                    <div className="min-w-0">
                      <p className="truncate font-mono text-sm text-ink-200">{artifact.storage_key.split("/").pop()}</p>
                      <p className="text-xs text-ink-500">
                        {artifact.kind} • {new Date(artifact.uploaded_at).toLocaleString()}
                      </p>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleDownloadArtifact(artifact)}
                      isLoading={actionLoading === `download-${artifact.id}`}
                      disabled={!!actionLoading}
                    >
                      Download
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="text-center text-xs text-ink-500">
            Powered by <span className="font-mono font-semibold text-gold-500">Ganji</span> — viewing/funding by share link; client actions require sign-in.
          </div>
        </div>
      </div>

      <WalletLinkDialog
        open={!!fundTarget}
        title={`Link wallet to fund ${fundTarget?.label ?? "this deal"}`}
        walletURL={walletURL}
        walletKey={walletKey}
        keyLabel="Wallet admin key"
        keyPlaceholder="LNbits admin key"
        submitLabel="Pay hold invoice"
        error={walletError}
        isSubmitting={!!actionLoading}
        onClose={closeFundDialog}
        onWalletURLChange={setWalletURL}
        onWalletKeyChange={setWalletKey}
        onSubmit={handleFund}
      />
    </main>
  );
}
