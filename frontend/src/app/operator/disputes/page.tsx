"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { api, type DealView } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";
import { formatCurrency } from "@/lib/utils/format";

export default function OperatorDisputesPage() {
  const router = useRouter();
  const { tokens, user } = useAuth();
  const [disputes, setDisputes] = useState<DealView[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  useEffect(() => {
    if (!tokens) {
      router.push("/login");
      return;
    }
    if (user && !user.is_operator) {
      router.push("/dashboard");
      return;
    }
    fetchDisputes();
  }, [tokens, user, router]);

  async function fetchDisputes() {
    if (!tokens) return;
    try {
      setIsLoading(true);
      const res = await api.listDisputes(tokens.access_token);
      setDisputes(res.disputes);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load disputes");
    } finally {
      setIsLoading(false);
    }
  }

  async function handleResolve(dealId: string, resolution: "release" | "refund") {
    if (!tokens) return;
    setActionLoading(dealId + resolution);
    try {
      await api.resolveDispute(dealId, resolution, tokens.access_token);
      await fetchDisputes();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Resolve failed");
    } finally {
      setActionLoading(null);
    }
  }

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
          <Link href="/dashboard" className="logo-gnj-sm" aria-label="Ganji Dashboard">GNJ</Link>
          <nav className="flex items-center gap-4">
            <span className="text-sm text-ink-300 hidden sm:block">{user?.display_name}</span>
          </nav>
        </div>
      </header>
      <div className="flex-1 px-6 py-10 md:py-16">
        <div className="mx-auto max-w-5xl">
          <Link href="/dashboard" className="inline-flex items-center gap-2 text-sm text-ink-500 hover:text-ink-300 mb-6 block">
            ← Dashboard
          </Link>
          <h1 className="font-display text-3xl font-semibold text-ink-100 mb-6">Dispute Queue</h1>

          {error && <div className="vault-card p-6 border-red-600/30 text-red-400 mb-6">{error}</div>}

          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
            </div>
          ) : disputes.length === 0 ? (
            <div className="vault-card p-8 text-center text-ink-500">No disputed deals.</div>
          ) : (
            <div className="space-y-4">
              {disputes.map((d) => (
                <div key={d.id} className="vault-card p-6 vault-seam">
                  <div className="flex items-start justify-between gap-4 mb-3">
                    <div>
                      <h3 className="font-display text-lg font-semibold text-ink-100">{d.title}</h3>
                      <p className="text-sm text-ink-500 font-mono">{d.client_email}</p>
                    </div>
                    <span className="text-sm font-mono text-ink-300">{formatCurrency(d.amount_sats)}</span>
                  </div>
                  {d.dispute_reason && (
                    <p className="text-sm text-ink-300 mb-4">Reason: {d.dispute_reason}</p>
                  )}
                  <div className="flex gap-3">
                    <Button
                      onClick={() => handleResolve(d.id, "release")}
                      disabled={actionLoading === d.id + "release"}
                      isLoading={actionLoading === d.id + "release"}
                    >
                      Release to freelancer
                    </Button>
                    <Button
                      variant="danger"
                      onClick={() => handleResolve(d.id, "refund")}
                      disabled={actionLoading === d.id + "refund"}
                      isLoading={actionLoading === d.id + "refund"}
                    >
                      Refund to client
                    </Button>
                    <Link href={`/operator/deals/${d.id}/reconcile`}>
                      <Button variant="ghost" size="sm">Reconcile</Button>
                    </Link>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </main>
  );
}