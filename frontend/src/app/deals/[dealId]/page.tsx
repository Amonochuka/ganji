"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { DealCard } from "@/components/deals/deal-card";
import { api, type DealView, type DealStatus, type DealDetailResponse, type SubmitWorkResponse, type DisputeDealResponse } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";
import { formatCurrency } from "@/lib/utils/format";

export default function DealDetailPage() {
  const router = useRouter();
  const params = useParams();
  const { tokens, user } = useAuth();
  const dealId = params.dealId as string;

  const [deal, setDeal] = useState<DealView | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  useEffect(() => {
    if (!tokens) {
      router.push("/login");
      return;
    }
    fetchDeal();
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

  async function handleAction(action: string, fn: () => Promise<void>) {
    setActionLoading(action);
    try {
      await fn();
      await fetchDeal();
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
  const canSubmit = isFreelancer && (deal.status === "locked" || deal.status === "awaiting_payment");
  const canDispute = isFreelancer && (deal.status === "locked" || deal.status === "work_submitted" || deal.status === "reviewing");
  const canRotateLink = isFreelancer;

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
                    {deal.status === "released" && (
                      <span className="flex items-center px-3 py-2 text-sm text-green-400 bg-green-600/10 rounded-lg border border-green-600/30">
                        ✅ Released — {formatCurrency(deal.amount_sats)} paid to your invoice
                      </span>
                    )}
                  </div>
                </div>
              )}

              {!isFreelancer && deal.status === "work_submitted" && (
                <div className="vault-card p-6 vault-seam border-gold-500/30 bg-gold-500/5">
                  <h2 className="font-display text-lg font-semibold text-gold-400 mb-2">Client Action Required</h2>
                  <p className="text-ink-300 mb-4">Review the delivered work and approve to release funds.</p>
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
                </div>
              )}
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
        </div>
      </div>
    </main>
  );
}

function DealHeader({ user }: { user: { display_name: string; id: string } | null }) {
  return (
    <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
        <Link href="/dashboard" className="logo-gnj-sm" aria-label="Ganji Dashboard">
          GNJ
        </Link>
        <nav className="flex items-center gap-4">
          <span className="text-sm text-ink-300 hidden sm:block">{user?.display_name}</span>
          <Button variant="ghost" className="h-10 px-4" onClick={() => window.location.href = "/"}>
            Log out
          </Button>
        </nav>
      </div>
    </header>
  );
}

function getStatusLabel(status: DealStatus): string {
  const labels: Record<DealStatus, string> = {
    awaiting_payment: "Awaiting Payment",
    locked: "Locked",
    work_submitted: "Work Submitted",
    reviewing: "Under Review",
    released: "Released",
    disputed: "Disputed",
    refunded: "Refunded",
  };
  return labels[status];
}

function getStatusIcon(status: DealStatus): string {
  const icons: Record<DealStatus, string> = {
    awaiting_payment: "⏳",
    locked: "🔒",
    work_submitted: "📤",
    reviewing: "👁️",
    released: "✅",
    disputed: "⚠️",
    refunded: "↩️",
  };
  return icons[status];
}

function getStatusClass(status: DealStatus): string {
  const classes: Record<DealStatus, string> = {
    awaiting_payment: "bg-yellow-600/20 text-yellow-400 border-yellow-600/30",
    locked: "bg-blue-600/20 text-blue-400 border-blue-600/30",
    work_submitted: "bg-purple-600/20 text-purple-400 border-purple-600/30",
    reviewing: "bg-indigo-600/20 text-indigo-400 border-indigo-600/30",
    released: "bg-green-600/20 text-green-400 border-green-600/30",
    disputed: "bg-red-600/20 text-red-400 border-red-600/30",
    refunded: "bg-gray-600/20 text-gray-400 border-gray-600/30",
  };
  return classes[status];
}

function StatusTimeline({ deal }: { deal: DealView }) {
  const steps = [
    { key: "created", label: "Deal Created", time: deal.created_at, completed: true },
    { key: "awaiting_payment", label: "Awaiting Payment", completed: deal.status !== "awaiting_payment" },
    { key: "locked", label: "Funds Locked", completed: ["locked", "work_submitted", "reviewing", "released", "disputed"].includes(deal.status) },
    { key: "work_submitted", label: "Work Submitted", completed: ["work_submitted", "reviewing", "released", "disputed"].includes(deal.status) },
    { key: "reviewing", label: "Under Review", completed: ["reviewing", "released", "disputed"].includes(deal.status) },
    { key: "released", label: "Released & Paid", completed: deal.status === "released" },
    { key: "disputed", label: "Disputed", completed: deal.status === "disputed" },
    { key: "refunded", label: "Refunded", completed: deal.status === "refunded" },
  ];

  return (
    <div className="space-y-4">
      {steps.map((step, i) => (
        <div key={step.key} className="flex gap-3">
          <div className="flex flex-col items-center">
            <div className={`w-3 h-3 rounded-full border-2 ${step.completed ? "bg-gold-500 border-gold-500" : "border-vault-border bg-vault-bg"}`} />
            {i < steps.length - 1 && (
              <div className={`w-0.5 h-full ${step.completed ? "bg-gold-500" : "bg-vault-border"}`} />
            )}
          </div>
          <div className="flex-1 pt-1">
            <p className={`text-sm ${step.completed ? "text-ink-100" : "text-ink-500"}`}>{step.label}</p>
            {step.time && step.completed && (
              <p className="text-xs text-ink-500 font-mono">{new Date(step.time).toLocaleString()}</p>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}