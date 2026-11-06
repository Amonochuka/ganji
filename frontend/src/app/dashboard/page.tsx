"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { DealCard } from "@/components/deals/deal-card";
import { api, type DealView, type ListDealsResponse } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";

export default function Dashboard() {
  const router = useRouter();
  const { user, tokens, isLoading: authLoading, logout } = useAuth();
  const [deals, setDeals] = useState<DealView[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!authLoading && !tokens) {
      router.push("/login");
      return;
    }
    if (tokens) {
      fetchDeals();
    }
  }, [authLoading, tokens, router]);

  async function fetchDeals() {
    if (!tokens) return;
    try {
      setIsLoading(true);
      const res = await api.listDeals(tokens.access_token);
      setDeals(res.deals);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load deals");
    } finally {
      setIsLoading(false);
    }
  }

  if (authLoading || (tokens && isLoading)) {
    return (
      <main className="flex min-h-screen flex-col bg-vault-bg">
        <DashboardHeader user={user} />
        <div className="flex-1 flex items-center justify-center px-6">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
        </div>
      </main>
    );
  }

  if (!tokens) return null;

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <DashboardHeader user={user} onLogout={() => { logout(); router.push("/"); }} />

      <div className="flex-1 px-6 py-8 md:py-12">
        <div className="mx-auto max-w-6xl">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-8">
            <div>
              <h1 className="font-display text-3xl font-semibold text-ink-100">Dashboard</h1>
              <p className="mt-1 text-sm text-ink-500">
                {deals.length} deal{deals.length !== 1 ? "s" : ""} • Manage your escrows
              </p>
            </div>
            <Link href="/deals/new">
              <Button size="lg">
                Create Deal
              </Button>
            </Link>
          </div>

          {error && (
            <div className="mb-6 p-4 rounded-lg bg-red-600/10 border border-red-600/30 text-red-400 text-sm">
              {error}
            </div>
          )}

          {deals.length === 0 ? (
            <div className="vault-card p-12 text-center">
              <div className="mx-auto mb-4 w-16 h-16 rounded-full bg-vault-raised flex items-center justify-center text-3xl">
                📭
              </div>
              <h2 className="font-display text-xl font-semibold text-ink-100 mb-2">No deals yet</h2>
              <p className="text-ink-500 mb-6 max-w-sm mx-auto">
                Create your first escrow deal to get started. Funds stay locked until your client approves.
              </p>
              <Link href="/deals/new">
                <Button size="lg">Create Your First Deal</Button>
              </Link>
            </div>
          ) : (
            <div className="space-y-4">
              {deals.map((deal) => (
                <DealCard
                  key={deal.id}
                  deal={deal}
                  onClick={() => router.push(`/deals/${deal.id}`)}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </main>
  );
}

function DashboardHeader({ user, onLogout }: { user: { display_name: string; slug?: string; is_operator?: boolean } | null; onLogout?: () => void }) {
  return (
    <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
      <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-6">
        <Link href="/dashboard" className="logo-gnj-sm" aria-label="Ganji Dashboard">
          GNJ
        </Link>
        <nav className="flex items-center gap-2">
          {user?.slug && (
            <Link href={`/cv/${user.slug}`} className="text-sm text-ink-300 hover:text-gold-400">
              My CV
            </Link>
          )}
          <Link href="/settings" className="text-sm text-ink-300 hover:text-gold-400">
            Settings
          </Link>
          {user?.is_operator && (
            <Link href="/operator/disputes" className="text-sm text-ink-300 hover:text-gold-400">
              Disputes
            </Link>
          )}
          <span className="text-sm text-ink-300">
            {user?.display_name}
          </span>
          {onLogout && (
            <Button variant="ghost" className="h-10 px-4" onClick={onLogout}>
              Log out
            </Button>
          )}
        </nav>
        <button
          className="md:hidden flex items-center gap-2 p-2 rounded-lg bg-vault-raised text-ink-300 hover:bg-vault-border"
          onClick={() => window.alert("Menu clicked")}
          aria-label="Menu"
        >
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <line x1="3" y1="6" x2="21" y2="6" />
            <line x1="3" y1="12" x2="21" y2="12" />
            <line x1="3" y1="18" x2="21" y2="18" />
          </svg>
        </button>
      </div>
    </header>
  );
}