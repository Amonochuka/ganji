"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { api, type CVProfile, type CVEntry } from "@/lib/api/api-client";
import { formatCurrency } from "@/lib/utils/format";

export default function CVProfilePage() {
  const params = useParams();
  const slug = params.slug as string;
  const [profile, setProfile] = useState<CVProfile | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    (async () => {
      try {
        setIsLoading(true);
        const res = await api.getCVProfile(slug);
        setProfile(res.profile);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load CV");
      } finally {
        setIsLoading(false);
      }
    })();
  }, [slug]);

  if (isLoading) {
    return (
      <main className="flex min-h-screen flex-col bg-vault-bg">
        <Header />
        <div className="flex-1 flex items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
        </div>
      </main>
    );
  }

  if (!profile || error) {
    return (
      <main className="flex min-h-screen flex-col bg-vault-bg">
        <Header />
        <div className="flex-1 flex items-center justify-center px-6">
          <div className="text-center">
            <p className="text-ink-500 mb-4">{error || "CV not found"}</p>
            <Link href="/">
              <Button variant="ghost">Home</Button>
            </Link>
          </div>
        </div>
      </main>
    );
  }

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <Header />
      <div className="flex-1 px-6 py-10 md:py-16">
        <div className="mx-auto max-w-4xl">
          <div className="vault-card p-8 vault-seam mb-8">
            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
              <div>
                <h1 className="font-display text-3xl font-semibold text-ink-100">{profile.display_name}</h1>
                <p className="text-sm text-ink-500 font-mono mt-1">@{profile.slug}</p>
              </div>
              <div className="text-right">
                <p className="text-xs text-ink-500 uppercase tracking-wide">Trust Score</p>
                <p className="font-digital text-3xl text-gold-400">{profile.trust_score}</p>
              </div>
            </div>
          </div>

          <h2 className="font-display text-xl font-semibold text-ink-100 mb-4">Verified Work</h2>
          {profile.entries.length === 0 ? (
            <div className="vault-card p-8 text-center text-ink-500">No verified entries yet.</div>
          ) : (
            <div className="space-y-4">
              {profile.entries.map((e) => (
                <EntryCard key={e.id} entry={e} slug={profile.slug} />
              ))}
            </div>
          )}
        </div>
      </div>
    </main>
  );
}

function EntryCard({ entry, slug }: { entry: CVEntry; slug: string }) {
  return (
    <div className="vault-card p-6 vault-seam">
      <div className="flex items-start justify-between gap-4 mb-3">
        <div>
          <h3 className="font-display text-lg font-semibold text-ink-100">{entry.deal_title}</h3>
          <p className="text-xs text-ink-500 font-mono mt-1">
            {entry.source_platform} · {entry.artifact_kind}
          </p>
        </div>
        <span className="text-sm font-mono text-ink-300">{formatCurrency(entry.amount_sats)}</span>
      </div>
      <dl className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
        <div>
          <dt className="text-ink-500">Hash (SHA-256)</dt>
          <dd className="font-mono text-xs text-ink-300 break-all">{entry.hash}</dd>
        </div>
        <div>
          <dt className="text-ink-500">Algorithm</dt>
          <dd className="font-mono text-xs text-ink-300">{entry.algorithm}</dd>
        </div>
        <div>
          <dt className="text-ink-500">Verified at</dt>
          <dd className="font-mono text-xs text-ink-300">{new Date(entry.verified_at).toLocaleString()}</dd>
        </div>
        <div>
          <dt className="text-ink-500">OTS</dt>
          <dd className="font-mono text-xs text-ink-300">
            {entry.ots_confirmed_at ? "Confirmed" : entry.ots_submitted_at ? "Pending" : "Not submitted"}
          </dd>
        </div>
      </dl>
      <div className="mt-4">
        <Link href={`/cv/${slug}/verify/${entry.id}`} className="text-sm text-gold-500 hover:text-gold-400 underline underline-offset-2">
          Verify this entry →
        </Link>
      </div>
    </div>
  );
}

function Header() {
  return (
    <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
        <Link href="/" className="logo-gnj-sm" aria-label="Ganji Home">GNJ</Link>
      </div>
    </header>
  );
}