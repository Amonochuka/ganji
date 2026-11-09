"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { api, type PublicDeal, type DealStatus, type MilestoneView } from "@/lib/api/api-client";
import { formatCurrency } from "@/lib/utils/format";

export default function PublicDealPage() {
  const params = useParams();
  const shareToken = params.shareToken as string;

  const [deal, setDeal] = useState<PublicDeal | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

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
    } catch (err) {
      if (!silent) setError(err instanceof Error ? err.message : "Failed to load deal");
    } finally {
      if (!silent) setIsLoading(false);
    }
  }

  async function runAction(key: string, fn: () => Promise<void>) {
    setActionLoading(key);
    setActionError(null);
    try {
      await fn();
      await fetchDeal(true);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : `Failed: ${key}`);
      await fetchDeal(true);
    } finally {
      setActionLoading(null);
    }
  }

  async function handleApprove() {
    await runAction("approve", async () => {
      const res = await api.publicApproveDeal(shareToken);
      setDeal(res.deal);
    });
  }

  async function handleAcceptNext(milestoneId: string) {
    await runAction(`accept-${milestoneId}`, async () => {
      await api.publicAcceptNextMilestone(shareToken, milestoneId);
    });
  }

  async function handleReject(milestoneId: string) {
    const reason = prompt("Reason for rejection (required):");
    if (!reason?.trim()) return;
    await runAction(`reject-${milestoneId}`, async () => {
      await api.publicRejectMilestone(shareToken, milestoneId, reason.trim());
    });
  }

  async function handleExit() {
    if (!window.confirm("Exit this deal? If the current milestone has not been submitted/approved, its escrow for that milestone will not be released to the freelancer (refunded/cancelled per policy).")) return;
    await runAction("exit", async () => {
      await api.publicExitDeal(shareToken);
    });
  }

  if (isLoading) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center bg-vault-bg px-6">
        <div className="w-full max-w-xl text-center">
          <Link href="/" className="logo-gnj inline-block mb-8" aria-label="Ganji Home">GNJ</Link>
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
          <Link href="/" className="logo-gnj inline-block mb-8" aria-label="Ganji Home">GNJ</Link>
          <div className="vault-card p-8 vault-seam">
            <div className="text-4xl mb-4">🔍</div>
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
  const canApproveDeal = deal.status === "work_submitted" || deal.status === "locked";
  const milestones = deal.milestones || [];
  const hasMilestones = milestones.length > 0;
  const terminal = isPaid || isRefunded;

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-3xl items-center justify-between px-6">
          <Link href="/" className="logo-gnj-sm" aria-label="Ganji Home">GNJ</Link>
          <span className="text-xs text-ink-500">Client review (share link)</span>
        </div>
      </header>

      <div className="flex-1 px-6 py-10">
        <div className="mx-auto w-full max-w-xl space-y-6">
          <div className="text-center">
            <span className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium border ${getStatusClass(deal.status)}`}>
              {getStatusIcon(deal.status)} {getStatusLabel(deal.status)}
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
                <p className="font-medium text-red-400 mb-1">⚠️ Dispute / Issue</p>
                <p className="text-sm text-ink-300">{deal.dispute_reason}</p>
              </div>
            )}

            {isPaid && (
              <div className="mb-6 p-4 rounded-lg bg-green-600/10 border border-green-600/30 text-center">
                <p className="font-medium text-green-400 mb-1">✅ Payment Released</p>
                <p className="text-sm text-ink-300">Escrow released to the freelancer.</p>
              </div>
            )}
            {isRefunded && (
              <div className="mb-6 p-4 rounded-lg bg-gray-600/10 border border-gray-600/30 text-center">
                <p className="font-medium text-gray-400 mb-1">↩️ Refunded / Cancelled</p>
                <p className="text-sm text-ink-300">Funds returned per policy for the exited/unfunded portion.</p>
              </div>
            )}

            {actionError && <p className="mb-4 text-sm text-red-400">{actionError}</p>}

            {!terminal && !isDisputed && (
              <div className="space-y-4">
                <div className="p-4 rounded-lg bg-vault-raised border border-vault-border">
                  <p className="text-xs text-ink-500 uppercase tracking-wide mb-2">Lightning Invoice (current escrow)</p>
                  <div className="flex gap-2">
                    <input type="text" readOnly value={deal.invoice} className="flex-1 rounded-lg border border-vault-border bg-vault-bg px-3 py-2 text-xs font-mono text-ink-400" />
                    <Button variant="ghost" size="sm" onClick={() => navigator.clipboard.writeText(deal.invoice)}>Copy</Button>
                  </div>
                  <p className="mt-2 text-xs text-ink-500">
                    Per-milestone invoices/payments are applied sequentially (M1 → M2…). Escrow settlement wiring is being finalized; actions below manage milestone state/review.
                  </p>
                </div>

                {canApproveDeal && !hasMilestones && (
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
            <p className="text-xs text-ink-500 mb-4">Review step-by-step. After approving M1, you can approve/start M2 (sequential per-milestone funding).</p>
            {!hasMilestones ? (
              <p className="text-sm text-ink-500">No milestones defined yet.</p>
            ) : (
              <div className="space-y-3">
                {milestones.map((ms, i) => (
                  <MilestoneRow
                    key={ms.id}
                    ms={ms}
                    isLast={i === milestones.length - 1}
                    canAct={!terminal && !isDisputed}
                    actionLoading={actionLoading}
                    onAcceptNext={() => handleAcceptNext(ms.id)}
                    onReject={() => handleReject(ms.id)}
                  />
                ))}
              </div>
            )}
            {!terminal && (
              <div className="mt-5 pt-4 border-t border-vault-border">
                <Button variant="danger" className="w-full" onClick={handleExit} isLoading={actionLoading === "exit"} disabled={!!actionLoading}>
                  Exit Deal
                </Button>
                <p className="mt-2 text-xs text-ink-500">Exit mid-milestone (work not submitted/approved): that milestone is not paid to the freelancer; funds refunded/cancelled per policy. Same symmetry applies to freelancer exits.</p>
              </div>
            )}
          </div>

          <div className="text-center text-xs text-ink-500">
            Powered by <span className="font-mono font-semibold text-gold-500">Ganji</span> — actions authorized by your share link.
          </div>
        </div>
      </div>
    </main>
  );
}

function MilestoneRow({ ms, isLast, canAct, actionLoading, onAcceptNext, onReject }: {
  ms: MilestoneView; isLast: boolean; canAct: boolean; actionLoading: string | null;
  onAcceptNext: () => void; onReject: () => void;
}) {
  const actionable = canAct && (ms.status === "in_progress" || ms.status === "pending" || ms.status === "completed");
  return (
    <div className="p-4 rounded-lg border border-vault-border bg-vault-raised/40">
      <div className="flex justify-between items-start gap-3">
        <div>
          <h3 className="font-medium text-ink-100">#{ms.order} {ms.title}</h3>
          <p className="text-sm text-ink-500">{formatCurrency(ms.amount_sats)}</p>
        </div>
        <span className={`inline-flex items-center px-2 py-1 rounded text-xs font-medium ${msStatusClass(ms.status)}`}>{msStatusLabel(ms.status)}</span>
      </div>
      {ms.completed_at && <p className="mt-1 text-xs text-ink-500 font-mono">Completed: {new Date(ms.completed_at).toLocaleString()}</p>}
      {actionable && (
        <div className="mt-3 flex flex-wrap gap-2">
          {!isLast && (ms.status === "in_progress" || ms.status === "completed" || ms.status === "pending") && (
            <Button size="sm" onClick={onAcceptNext} isLoading={actionLoading === `accept-${ms.id}`} disabled={!!actionLoading}>
              Approve & Start Next
            </Button>
          )}
          {isLast && (ms.status === "in_progress" || ms.status === "completed") && (
            <span className="text-xs text-ink-500">Final milestone — use deal approval/release when wired for milestone payouts.</span>
          )}
          {ms.status !== "exited" && ms.status !== "rejected" && (
            <Button size="sm" variant="outline" onClick={onReject} isLoading={actionLoading === `reject-${ms.id}`} disabled={!!actionLoading}>
              Reject
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

function msStatusLabel(s: string) {
  return ({ pending: "Pending", in_progress: "In Progress", completed: "Completed", exited: "Exited", rejected: "Rejected" } as Record<string,string>)[s] || s;
}
function msStatusClass(s: string) {
  return ({ pending: "bg-yellow-600/20 text-yellow-400", in_progress: "bg-blue-600/20 text-blue-400", completed: "bg-green-600/20 text-green-400", exited: "bg-red-600/20 text-red-400", rejected: "bg-gray-600/20 text-gray-400" } as Record<string,string>)[s] || "bg-gray-600/20 text-gray-400";
}
function getStatusLabel(status: DealStatus): string {
  return ({ awaiting_payment: "Awaiting Payment", locked: "Funds Locked", work_submitted: "Work Submitted", reviewing: "Under Review", released: "Released", disputed: "Disputed", refunded: "Refunded" } as Record<DealStatus,string>)[status];
}
function getStatusIcon(status: DealStatus): string {
  return ({ awaiting_payment: "⏳", locked: "🔒", work_submitted: "📤", reviewing: "👁️", released: "✅", disputed: "⚠️", refunded: "↩️" } as Record<DealStatus,string>)[status];
}
function getStatusClass(status: DealStatus): string {
  return ({ awaiting_payment: "bg-yellow-600/20 text-yellow-400 border-yellow-600/30", locked: "bg-blue-600/20 text-blue-400 border-blue-600/30", work_submitted: "bg-purple-600/20 text-purple-400 border-purple-600/30", reviewing: "bg-indigo-600/20 text-indigo-400 border-indigo-600/30", released: "bg-green-600/20 text-green-400 border-green-600/30", disputed: "bg-red-600/20 text-red-400 border-red-600/30", refunded: "bg-gray-600/20 text-gray-400 border-gray-600/30" } as Record<DealStatus,string>)[status];
}
