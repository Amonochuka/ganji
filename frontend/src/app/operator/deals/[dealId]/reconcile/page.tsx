"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { api, type DealView } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";

export default function ReconcilePage() {
  const params = useParams();
  const router = useRouter();
  const dealId = params.dealId as string;
  const { tokens, user } = useAuth();
  const [deal, setDeal] = useState<DealView | null>(null);
  const [action, setAction] = useState<"confirm_payout" | "reset_payout">("reset_payout");
  const [payoutId, setPayoutId] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!tokens) {
      router.push("/login");
      return;
    }
    if (user && !user.is_operator) {
      router.push("/dashboard");
      return;
    }
    (async () => {
      try {
        setIsLoading(true);
        const res = await api.getDeal(dealId, tokens.access_token);
        setDeal(res.deal);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load deal");
      } finally {
        setIsLoading(false);
      }
    })();
  }, [tokens, user, dealId, router]);

  async function handleSubmit() {
    if (!tokens) return;
    setSubmitting(true);
    setError(null);
    setSuccess(null);
    try {
      const res = await api.reconcileDeal(dealId, action, action === "confirm_payout" ? payoutId : undefined, tokens.access_token);
      setDeal(res.deal);
      setSuccess("Reconciled successfully");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Reconcile failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
          <Link href="/dashboard" className="logo-gnj-sm" aria-label="Ganji Dashboard">GNJ</Link>
        </div>
      </header>
      <div className="flex-1 px-6 py-10 md:py-16">
        <div className="mx-auto max-w-2xl">
          <Link href="/operator/disputes" className="inline-flex items-center gap-2 text-sm text-ink-500 hover:text-ink-300 mb-6 block">
            ← Disputes
          </Link>
          <h1 className="font-display text-3xl font-semibold text-ink-100 mb-6">Reconcile Deal</h1>

          {isLoading && (
            <div className="flex items-center justify-center py-12">
              <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
            </div>
          )}

          {error && <div className="vault-card p-6 border-red-600/30 text-red-400 mb-6">{error}</div>}
          {success && <div className="vault-card p-6 border-green-600/30 text-green-400 mb-6">{success}</div>}

          {deal && (
            <div className="vault-card p-6 vault-seam space-y-4">
              <h2 className="font-display text-lg font-semibold text-ink-100">{deal.title}</h2>
              <p className="text-sm text-ink-500 font-mono">Status: {deal.status}</p>

              <div className="space-y-3">
                <label className="block text-sm text-ink-300">Action</label>
                <select
                  value={action}
                  onChange={(e) => setAction(e.target.value as "confirm_payout" | "reset_payout")}
                  className="w-full rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm text-ink-100"
                >
                  <option value="reset_payout">reset_payout — clear payout tracking, re-payable</option>
                  <option value="confirm_payout">confirm_payout — record verified payout, release</option>
                </select>
              </div>

              {action === "confirm_payout" && (
                <div className="space-y-3">
                  <label className="block text-sm text-ink-300">Payout checking ID (from LNbits)</label>
                  <input
                    type="text"
                    value={payoutId}
                    onChange={(e) => setPayoutId(e.target.value)}
                    className="w-full rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm font-mono text-ink-300"
                  />
                </div>
              )}

              <Button onClick={handleSubmit} disabled={submitting} isLoading={submitting}>
                Submit
              </Button>
            </div>
          )}
        </div>
      </div>
    </main>
  );
}