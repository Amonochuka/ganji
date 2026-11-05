"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { api, type VerifyResult } from "@/lib/api/api-client";

export default function VerifyEntryPage() {
  const params = useParams();
  const slug = params.slug as string;
  const entryId = params.entryId as string;
  const [result, setResult] = useState<VerifyResult | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    (async () => {
      try {
        setIsLoading(true);
        const res = await api.verifyEntry(slug, entryId);
        setResult(res.verification);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Verification failed");
      } finally {
        setIsLoading(false);
      }
    })();
  }, [slug, entryId]);

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
          <Link href="/" className="logo-gnj-sm" aria-label="Ganji Home">GNJ</Link>
        </div>
      </header>
      <div className="flex-1 px-6 py-10 md:py-16">
        <div className="mx-auto max-w-3xl">
          <Link href={`/cv/${slug}`} className="inline-flex items-center gap-2 text-sm text-ink-500 hover:text-ink-300 mb-6 block">
            ← Back to CV
          </Link>
          <h1 className="font-display text-3xl font-semibold text-ink-100 mb-6">Entry Verification</h1>

          {isLoading && (
            <div className="flex items-center justify-center py-12">
              <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
            </div>
          )}

          {error && (
            <div className="vault-card p-6 border-red-600/30 text-red-400">{error}</div>
          )}

          {result && (
            <div className="space-y-6">
              <div className={`vault-card p-6 vault-seam ${result.valid && result.matches_current ? "border-green-600/30" : "border-red-600/30"}`}>
                <div className="flex items-center gap-3 mb-4">
                  <span className="text-2xl">{result.valid && result.matches_current ? "✅" : "❌"}</span>
                  <div>
                    <p className="font-display text-lg font-semibold text-ink-100">
                      {result.valid && result.matches_current ? "Hash matches — entry intact" : "Hash mismatch — possible tampering"}
                    </p>
                    <p className="text-sm text-ink-500">Deal: {result.deal_title}</p>
                  </div>
                </div>
                <dl className="grid grid-cols-1 gap-3 text-sm">
                  <div>
                    <dt className="text-ink-500">Entry ID</dt>
                    <dd className="font-mono text-xs text-ink-300 break-all">{result.entry_id}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Slug</dt>
                    <dd className="font-mono text-xs text-ink-300">{result.slug}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Hash ({result.algorithm})</dt>
                    <dd className="font-mono text-xs text-ink-300 break-all">{result.hash}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Valid</dt>
                    <dd className="font-mono text-xs text-ink-300">{String(result.valid)}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Matches current</dt>
                    <dd className="font-mono text-xs text-ink-300">{String(result.matches_current)}</dd>
                  </div>
                  <div>
                    <dt className="text-ink-500">Verified at</dt>
                    <dd className="font-mono text-xs text-ink-300">{new Date(result.verified_at).toLocaleString()}</dd>
                  </div>
                </dl>
              </div>

              <div className="vault-card p-6 vault-seam">
                <h2 className="font-display text-lg font-semibold text-ink-100 mb-4">OpenTimestamps (Bitcoin anchor)</h2>
                <dl className="grid grid-cols-1 gap-3 text-sm">
                  <div>
                    <dt className="text-ink-500">OTS verified</dt>
                    <dd className="font-mono text-xs text-ink-300">{String(result.ots_verified)}</dd>
                  </div>
                  {result.ots_block_height ? (
                    <div>
                      <dt className="text-ink-500">Block height</dt>
                      <dd className="font-mono text-xs text-ink-300">{result.ots_block_height}</dd>
                    </div>
                  ) : null}
                  {result.ots_confirmed_at ? (
                    <div>
                      <dt className="text-ink-500">Confirmed at</dt>
                      <dd className="font-mono text-xs text-ink-300">{new Date(result.ots_confirmed_at).toLocaleString()}</dd>
                    </div>
                  ) : null}
                </dl>
                {result.ots_proof && (
                  <details className="mt-4">
                    <summary className="text-sm text-gold-500 cursor-pointer">Show OTS proof (base64-ish)</summary>
                    <pre className="mt-2 p-3 rounded bg-vault-raised text-xs font-mono text-ink-300 overflow-auto max-h-64">{result.ots_proof}</pre>
                  </details>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </main>
  );
}