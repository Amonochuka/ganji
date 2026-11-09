// @ts-nocheck
"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { DealCard } from "@/components/deals/deal-card";
import { api, type DealView, type DealStatus, type DealDetailResponse, type SubmitWorkResponse, type DisputeDealResponse, type MilestoneView } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";
import { formatCurrency } from "@/lib/utils/format";

export default function DealDetailPage() {
  const router = useRouter();
  const params = useParams();
  const { tokens, user, logout } = useAuth();
  const dealId = params.dealId as string;

  const [deal, setDeal] = useState<DealView | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [milestones, setMilestones] = useState<MilestoneView[]>([]);

  useEffect(() => {
    if (!tokens) {
      router.push("/login");
      return;
    }
    fetchDeal();
    fetchMilestones();
  }, [tokens, dealId, router]);

  async function fetchDeal() {
    if (!tokens) return;
    try {
      setIsLoading(true);
      const res = await api.getDeal(dealId, tokens.access_token);
      setDeal(res.deal);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load deal");
    } finally {
      setIsLoading(false);
    }
  }

  async function fetchMilestones() {
    if (!tokens) return;
    try {
      const res = await api.listMilestones(dealId, tokens.access_token);
      setMilestones(res.milestones);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load milestones");
    }
  }

  async function handleAction(action: string, fn: () => Promise<void>) {
    setActionLoading(action);
    try {
      await fn();
      await fetchDeal();
      await fetchMilestones();
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${action}`);
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

  async function handleDispute() {
    const reason = prompt("Dispute reason (required):");
    if (!reason?.trim()) return;
    await handleAction("dispute", async () => {
      const res = await api.disputeDeal(dealId, reason.trim(), tokens!.access_token);
      setDeal(res.deal);
    });
  }

  async function handleRotateShareLink() {
    await handleAction("rotate share link", async () => {
      const res = await api.rotateShareLink(dealId, tokens!.access_token);
      setDeal(res.deal);
    });
  }

  async function handleCopyShareLink() {
    if (!deal) return;
    const url = `${window.location.origin}/public/deals/${deal.share_token}`;
    await navigator.clipboard.writeText(url);
    setError("Share link copied to clipboard");
    setTimeout(() => setError(null), 3000);
  }

  async function handleApproveDeal() {
    await handleAction("approve", async () => {
      const res = await api.approveDeal(dealId, tokens!.access_token);
      setDeal(res.deal);
    });
  }

  async function handleExitDeal() {
    if (!window.confirm("Exit this deal? Mid-milestone exit (not submitted/approved) cancels that milestone hold per redesign.")) return;
    await handleAction("exit deal", async () => {
      await api.exitDeal(dealId, tokens!.access_token);
      await fetchDeal();
      await fetchMilestones();
    });
  }

  if (isLoading) {
    return (
      <main className="flex min-h-screen flex-col bg-vault-bg">
        <DealHeader user={user} />
        <div className="flex-1 flex items-center justify-center px-6">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
        </div>
      </main>
    );
  }

  if (!deal || error) {
    return (
      <main className="flex min-h-screen flex-col bg-vault-bg">
        <DealHeader user={user} />
        <div className="flex-1 flex items-center justify-center px-6">
          <div className="text-center">
            <p className="text-ink-500 mb-4">{error || "Deal not found"}</p>
            <Link href="/dashboard">
              <Button variant="ghost">Back to Dashboard</Button>
            </Link>
          </div>
        </div>
      </main>
    );
  }

  const isFreelancer = user?.id === deal.freelancer_id;
  const isClient = !!user && user.email.toLowerCase() === deal.client_email.toLowerCase();
  const canSubmit = isFreelancer && (deal.status === "locked" || deal.status === "awaiting_payment");
  const canDispute = isFreelancer && (deal.status === "locked" || deal.status === "work_submitted" || deal.status === "reviewing");
  const canRotateLink = isFreelancer;
  const clientCanApprove = ["work_submitted", "reviewing", "disputed"].includes(deal.status);
  const clientCanDispute = ["locked", "work_submitted", "reviewing"].includes(deal.status);

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <DealHeader user={user} />

      <div className="flex-1 px-6 py-8 md:py-12">
        <div className="mx-auto max-w-4xl">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-8">
            <div>
              <Link href="/dashboard" className="inline-flex items-center gap-2 text-sm text-ink-500 hover:text-ink-300 mb-2 block">
                ← Dashboard
              </Link>
              <h1 className="font-display text-3xl font-semibold text-ink-100">{deal.title}</h1>
            </div>
            <span className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium border ${getStatusClass(deal.status)}`}>
              {getStatusIcon(deal.status)} {getStatusLabel(deal.status)}
            </span>
          </div>

          {error && (
            <div className="mb-6 p-4 rounded-lg bg-yellow-600/10 border border-yellow-600/30 text-yellow-400 text-sm">
              {error}
            </div>
          )}

          <div className="grid lg:grid-cols-3 gap-6">
            <div className="lg:col-span-2 space-y-6">
              <DealCard deal={deal} />

              <div className="vault-card p-6 vault-seam">
                <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">Deal Details</h2>
                <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
                  <div>
                    <dt className="text-ink-500">Amount</dt>
                    <dd className="font-mono text-lg font-medium text-ink-100 mono-tabular">{formatCurrency(deal.amount_sats)}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Source Platform</dt>
                    <dd className="text-ink-300 font-mono">{deal.source_platform}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Client</dt>
                    <dd className="text-ink-300 font-mono truncate">{deal.client_email}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Freelancer</dt>
                    <dd className="text-ink-300 font-mono">{deal.freelancer_id === user?.id ? "You" : deal.freelancer_id}</dd>
                  </div>
                  <div className="sm:col-span-2">
                    <dt className="text-ink-500">Preimage Hash</dt>
                    <dd className="font-mono text-xs text-ink-400 break-all">{deal.preimage_hash}</dd>
                  </div>
                  <div className="sm:col-span-2">
                    <dt className="text-ink-500">LNbits Checking ID</dt>
                    <dd className="font-mono text-xs text-ink-400">{deal.checking_id || "—"}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Created</dt>
                    <dd className="text-ink-300 font-mono">{new Date(deal.created_at).toLocaleString()}</dd>
                  </div>
                  {deal.verified_at && (
                    <div>
                      <dt className="text-ink-500">Anchored to Bitcoin</dt>
                      <dd className="text-ink-300 font-mono text-green-400">{new Date(deal.verified_at).toLocaleString()}</dd>
                    </div>
                  )}
                </dl>
              </div>

              {deal.dispute_reason && (
                <div className="vault-card p-6 vault-seam border-red-600/30 bg-red-600/5">
                  <h2 className="font-display text-lg font-semibold text-red-400 mb-2 flex items-center gap-2">⚠️ Dispute</h2>
                  <p className="text-ink-300">{deal.dispute_reason}</p>
                  {deal.disputed_at && (
                    <p className="mt-2 text-xs text-ink-500 font-mono">Disputed at {new Date(deal.disputed_at).toLocaleString()}</p>
                  )}
                </div>
              )}

              {isFreelancer && (
                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">Actions</h2>
                  <div className="flex flex-wrap gap-3">
                    {canSubmit && (
                      <Button
                        onClick={handleSubmitWork}
                        disabled={actionLoading === "submit work"}
                        isLoading={actionLoading === "submit work"}
                      >
                        Submit Work
                      </Button>
                    )}
                    {canDispute && (
                      <Button
                        variant="outline"
                        onClick={handleDispute}
                        disabled={actionLoading === "dispute"}
                        isLoading={actionLoading === "dispute"}
                      >
                        Raise Dispute
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
                    {deal.status !== "released" && deal.status !== "refunded" && (
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
                        ✅ Released — {formatCurrency(deal.amount_sats)} paid to your invoice
                      </span>
                    )}
                  </div>
                  {deal.status !== "released" && deal.status !== "refunded" && (
                    <p className="text-xs text-ink-400 mt-3">Exit mid-milestone (not submitted/approved): that milestone’s escrow hold is cancelled/refunded per redesign.</p>
                  )}
                </div>
              )}

              {!isFreelancer && isClient && (
                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">Your Actions</h2>
                  {clientCanApprove && (
                    <div className="mb-4 p-4 rounded-lg bg-gold-500/5 border border-gold-500/30">
                      <p className="font-medium text-gold-400 mb-1">Review the delivered work</p>
                      <p className="text-sm text-ink-300">Approving releases escrow (current milestone in sequential mode) to the freelancer.</p>
                    </div>
                  )}
                  <div className="flex flex-wrap gap-3">
                    {clientCanApprove && (
                      <Button
                        onClick={() => handleAction("approve", async () => {
                          const res = await api.approveDeal(dealId, tokens!.access_token);
                          setDeal(res.deal);
                        })}
                        disabled={actionLoading === "approve"}
                        isLoading={actionLoading === "approve"}
                        size="lg"
                      >
                        Approve & Release
                      </Button>
                    )}
                    {clientCanDispute && (
                      <Button
                        variant="outline"
                        onClick={handleDispute}
                        disabled={actionLoading === "dispute"}
                        isLoading={actionLoading === "dispute"}
                      >
                        Raise Dispute
                      </Button>
                    )}
                    {deal.status === "released" && (
                      <span className="flex items-center px-3 py-2 text-sm text-green-400 bg-green-600/10 rounded-lg border border-green-600/30">
                        ✅ Payment released to the freelancer
                      </span>
                    )}
                    {deal.status === "refunded" && (
                      <span className="flex items-center px-3 py-2 text-sm text-gray-400 bg-gray-600/10 rounded-lg border border-gray-600/30">
                        ↩️ Escrow refunded
                      </span>
                    )}
                    {deal.status !== "released" && deal.status !== "refunded" && (
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
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">Milestones</h2>
                  {milestones.length === 0 ? (
                    <p className="text-ink-500">No milestones defined for this deal</p>
                  ) : (
                    <div className="space-y-4">
                      {milestones.map((ms) => (
                        <div key={ms.id} className="p-4 rounded-lg border transition-colors focus-within:outline-none focus-within:ring-2 focus-within:ring-gold-500">
                          <div className="flex justify-between items-start">
                            <div>
                              <h3 className="font-medium text-ink-100">{ms.title}</h3>
                              <p className="text-sm text-ink-500">Sats: {formatCurrency(ms.amount_sats)} • Order: #{ms.order}</p>
                            </div>
                            <span className={`inline-flex items-center gap-1.5 px-2 py-1 rounded text-xs font-medium ${getMilestoneStatusClass(ms.status)}`}>
                              {getMilestoneStatusLabel(ms.status)}
                            </span>
                          </div>
                          {ms.completed_at && (
                            <p className="mt-1 text-xs text-ink-500 font-mono">Completed: {new Date(ms.completed_at).toLocaleString()}</p>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">Timeline</h2>
                  <StatusTimeline deal={deal} />
                </div>

                <div className="vault-card p-6 vault-seam">
                  <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">Verification</h2>
                  <p className="text-sm text-ink-500 mb-3">
                    This deal will be anchored to Bitcoin via OpenTimestamps when released.
                  </p>
                  {deal.verified_at ? (
                    <div className="p-3 rounded-lg bg-green-600/10 border border-green-600/30 text-green-400 text-sm">
                      ✅ Verified on Bitcoin at {new Date(deal.verified_at).toLocaleString()}
                    </div>
                  ) : (
                    <div className="p-3 rounded-lg bg-yellow-600/10 border border-yellow-600/30 text-yellow-400 text-sm">
                      ⏳ Pending verification (anchors on release)
                    </div>
                  )}
                </div>
              </div>
            </div>

            <div className="space-y-6">
              <div className="vault-card p-6 vault-seam">
                <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">Share Link</h2>
                <p className="text-sm text-ink-500 mb-4">
                  Send this to your client. They can view the deal and approve payment.
                </p>
                <div className="space-y-3">
                  <div className="flex gap-2">
                    <input
                      type="text"
                      readOnly
                      value={`${window.location.origin}/public/deals/${deal.share_token}`}
                      className="flex-1 rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm font-mono text-ink-300"
                    />
                    <Button variant="ghost" size="sm" onClick={handleCopyShareLink}>
                      Copy
                    </Button>
                  </div>
                  <p className="text-xs text-ink-500">
                    Status: <span className="font-mono capitalize">{deal.status.replace("_", " ")}</span>
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


function DealHeader({ user }: { user: { email?: string } | null }) {
  return (
    <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
      <div className="mx-auto flex h-16 max-w-5xl items-center justify-between px-6">
        <Link href="/dashboard" className="logo-gnj-sm" aria-label="Ganji Dashboard">GNJ</Link>
        <span className="text-xs font-mono text-ink-500">{user?.email ?? ""}</span>
      </div>
    </header>
  );
}

function getStatusClass(status: string): string {
  const map: Record<string, string> = {
    awaiting_payment: "bg-yellow-600/10 text-yellow-400 border-yellow-600/30",
    locked: "bg-blue-600/10 text-blue-400 border-blue-600/30",
    work_submitted: "bg-gold-500/10 text-gold-400 border-gold-500/30",
    reviewing: "bg-gold-500/10 text-gold-400 border-gold-500/30",
    released: "bg-green-600/10 text-green-400 border-green-600/30",
    disputed: "bg-red-600/10 text-red-400 border-red-600/30",
    refunded: "bg-gray-600/10 text-gray-400 border-gray-600/30",
  };
  return map[status] || "bg-gray-600/10 text-gray-400 border-gray-600/30";
}
function getStatusIcon(status: string): string {
  const map: Record<string, string> = { awaiting_payment: "⏳", locked: "🔒", work_submitted: "📤", reviewing: "👀", released: "✅", disputed: "⚠️", refunded: "↩️" };
  return map[status] || "•";
}
function getStatusLabel(status: string): string {
  const map: Record<string, string> = { awaiting_payment: "Awaiting payment", locked: "Locked", work_submitted: "Work submitted", reviewing: "Reviewing", released: "Released", disputed: "Disputed", refunded: "Refunded" };
  return map[status] || status.replace(/_/g, " ");
}

function StatusTimeline({ deal }: { deal: DealView }) {
  const steps = ["awaiting_payment", "locked", "work_submitted", "released"];
  return (
    <ol className="space-y-2 text-sm">
      {steps.map((s) => (
        <li key={s} className={deal.status === s ? "text-gold-400 font-medium" : "text-ink-400"}>{getStatusLabel(s)}</li>
      ))}
      {deal.status === "disputed" && <li className="text-red-400 font-medium">Disputed</li>}
      {deal.status === "refunded" && <li className="text-gray-400 font-medium">Refunded</li>}
    </ol>
  );
}

function getMilestoneStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    pending: "Pending",
    awaiting_payment: "Awaiting payment",
    funded: "Funded",
    in_progress: "In Progress",
    work_submitted: "Submitted",
    completed: "Approved (payout pending)",
    released: "Released (paid)",
    exited: "Exited",
    rejected: "Rejected (under review)",
    disputed: "Under independent review",
    refunded: "Refunded",
  };
  return labels[status] || status;
}

function getMilestoneStatusClass(status: string): string {
  const classes: Record<string, string> = {
    pending: "bg-yellow-600/20 text-yellow-400 border-yellow-600/30",
    awaiting_payment: "bg-yellow-600/20 text-yellow-400 border-yellow-600/30",
    funded: "bg-blue-600/20 text-blue-400 border-blue-600/30",
    in_progress: "bg-blue-600/20 text-blue-400 border-blue-600/30",
    work_submitted: "bg-gold-500/20 text-gold-400 border-gold-500/30",
    completed: "bg-blue-600/20 text-blue-400 border-blue-600/30",
    released: "bg-green-600/20 text-green-400 border-green-600/30",
    exited: "bg-red-600/20 text-red-400 border-red-600/30",
    rejected: "bg-orange-600/20 text-orange-400 border-orange-600/30",
    disputed: "bg-red-600/20 text-red-400 border-red-600/30",
    refunded: "bg-gray-600/20 text-gray-400 border-gray-600/30",
  };
  return classes[status] || "bg-gray-600/20 text-gray-400 border-gray-600/30";
}
