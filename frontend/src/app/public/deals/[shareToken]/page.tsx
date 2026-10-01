"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { api, type PublicDeal, type DealStatus, type ApproveDealResponse } from "@/lib/api/api-client";
import { formatCurrency } from "@/lib/utils/format";

export default function PublicDealPage() {
  const params = useParams();
  const shareToken = params.shareToken as string;

  const [deal, setDeal] = useState<PublicDeal | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isApproving, setIsApproving] = useState(false);

  useEffect(() => {
    fetchDeal();
  }, [shareToken]);

  async function fetchDeal() {
    try {
      setIsLoading(true);
      const res = await api.getPublicDeal(shareToken);
      setDeal(res.deal);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load deal");
    } finally {
      setIsLoading(false);
    }
  }

  async function handleApprove() {
    if (!deal) return;
    setIsApproving(true);
    try {
      // The approve endpoint requires the deal ID, not share token
      // But the public deal doesn't expose the internal ID
      // We need to check - the backend approve endpoint is POST /deals/:dealID/approve
      // and it uses email from auth context. For public access, we might need a different flow.
      // Let me check the backend - the ApproveDeal handler uses email from context
      // For public access, we need to handle this differently.
      // Actually, looking at the backend: ApproveDeal gets email from c.GetString("email")
      // which comes from the auth middleware. So public approval would need auth.
      // But the public deal page should allow the client to approve without login.
      // This might need a backend change or a different approach.
      // For now, let's assume there's a public approve endpoint or we need to handle this.
      
      // Actually, looking at the router.go, the approve route is in the protected group
      // So it requires auth. But the client might not have an account.
      // We might need to add a public approve endpoint that takes the share token.
      // For now, I'll show a message that they need to log in or we'll need to add a public endpoint.
      
      setError("Approval requires authentication. Please contact the freelancer for a direct payment link.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to approve");
    } finally {
      setIsApproving(false);
    }
  }

  if (isLoading) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center bg-vault-bg px-6">
        <div className="w-full max-w-xl text-center">
          <Link href="/" className="logo-gnj inline-block mb-8" aria-label="Ganji Home">
            GNJ
          </Link>
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
          <Link href="/" className="logo-gnj inline-block mb-8" aria-label="Ganji Home">
            GNJ
          </Link>
          <div className="vault-card p-8 vault-seam">
            <div className="text-4xl mb-4">🔍</div>
            <h1 className="font-display text-2xl font-semibold text-ink-100 mb-2">Deal Not Found</h1>
            <p className="text-ink-500 mb-6">{error || "This deal link is invalid or has expired."}</p>
            <Link href="/">
              <Button variant="ghost">Go to Ganji</Button>
            </Link>
          </div>
        </div>
      </main>
    );
  }

  const isPaid = deal.status === "released";
  const isDisputed = deal.status === "disputed";
  const isRefunded = deal.status === "refunded";
  const canApprove = deal.status === "work_submitted" || deal.status === "locked";

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-3xl items-center justify-between px-6">
          <Link href="/" className="logo-gnj-sm" aria-label="Ganji Home">
            GNJ
          </Link>
        </div>
      </header>

      <div className="flex-1 flex items-center justify-center px-6 py-12">
        <div className="w-full max-w-xl">
          <div className="text-center mb-8">
            <Link href="/" className="logo-gnj inline-block mb-6" aria-label="Ganji Home">
              GNJ
            </Link>
            <span className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium border ${getStatusClass(deal.status)}`}>
              {getStatusIcon(deal.status)} {getStatusLabel(deal.status)}
            </span>
          </div>

          <div className="vault-card p-6 md:p-8 vault-seam">
            <h1 className="font-display text-2xl md:text-3xl font-semibold text-ink-100 mb-2">{deal.title}</h1>
            <p className="text-ink-500 mb-6 font-mono">{deal.source_platform}</p>

            <div className="mb-6 p-4 rounded-lg bg-vault-raised border border-vault-border">
              <p className="text-xs text-ink-500 uppercase tracking-wide mb-1">Amount</p>
              <p className="font-mono text-3xl font-medium text-ink-100 mono-tabular">{formatCurrency(deal.amount_sats)}</p>
            </div>

            <dl className="space-y-4 mb-6 text-sm">
              <div className="flex justify-between">
                <dt className="text-ink-500">Status</dt>
                <dd className="font-mono text-ink-300 capitalize">{deal.status.replace("_", " ")}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-ink-500">Created</dt>
                <dd className="font-mono text-ink-300">{new Date(deal.created_at).toLocaleDateString()}</dd>
              </div>
            </dl>

            {deal.dispute_reason && (
              <div className="mb-6 p-4 rounded-lg bg-red-600/10 border border-red-600/30">
                <p className="font-medium text-red-400 mb-1 flex items-center gap-2">⚠️ Dispute Raised</p>
                <p className="text-sm text-ink-300">{deal.dispute_reason}</p>
              </div>
            )}

            {isPaid && (
              <div className="mb-6 p-4 rounded-lg bg-green-600/10 border border-green-600/30 text-center">
                <p className="font-medium text-green-400 mb-1">✅ Payment Released</p>
                <p className="text-sm text-ink-300">The freelancer has been paid {formatCurrency(deal.amount_sats)} via Lightning.</p>
              </div>
            )}

            {isRefunded && (
              <div className="mb-6 p-4 rounded-lg bg-gray-600/10 border border-gray-600/30 text-center">
                <p className="font-medium text-gray-400 mb-1">↩️ Refunded to Client</p>
                <p className="text-sm text-ink-300">The escrow has been cancelled and funds returned.</p>
              </div>
            )}

            {!isPaid && !isRefunded && (
              <div className="space-y-3">
                <div className="p-4 rounded-lg bg-vault-raised border border-vault-border">
                  <p className="text-xs text-ink-500 uppercase tracking-wide mb-2">Lightning Invoice</p>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      readOnly
                      value={deal.invoice}
                      className="flex-1 rounded-lg border border-vault-border bg-vault-bg px-3 py-2 text-xs font-mono text-ink-400"
                    />
                    <Button variant="ghost" size="sm" onClick={() => navigator.clipboard.writeText(deal.invoice)}>
                      Copy
                    </Button>
                  </div>
                  <p className="mt-2 text-xs text-ink-500">
                    Pay this invoice to fund the escrow. Funds are held until you approve delivery.
                  </p>
                </div>

                {canApprove && (
                  <Button
                    size="lg"
                    className="w-full py-3.5"
                    onClick={handleApprove}
                    isLoading={isApproving}
                  >
                    Approve & Release Payment
                  </Button>
                )}
              </div>
            )}

            <div className="mt-6 pt-6 border-t border-vault-border text-center">
              <p className="text-xs text-ink-500">
                Powered by <span className="font-mono font-semibold text-gold-500">Ganji</span> — Lightning Escrow Protocol
              </p>
              <p className="mt-1 text-xs text-ink-700">
                No platform lock-in. Cryptographically verifiable.
              </p>
            </div>
          </div>
        </div>
      </div>
    </main>
  );
}

function getStatusLabel(status: DealStatus): string {
  const labels: Record<DealStatus, string> = {
    awaiting_payment: "Awaiting Payment",
    locked: "Funds Locked",
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