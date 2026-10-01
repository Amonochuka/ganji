"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { FormBanner } from "@/components/ui/form-banner";
import { api, type CreateDealRequest } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";

export default function CreateDealPage() {
  const router = useRouter();
  const { tokens } = useAuth();

  const [title, setTitle] = useState("");
  const [amountSats, setAmountSats] = useState("");
  const [sourcePlatform, setSourcePlatform] = useState("");
  const [clientEmail, setClientEmail] = useState("");
  const [payeeInvoice, setPayeeInvoice] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const platforms = [
    "Upwork",
    "Fiverr",
    "Freelancer.com",
    "Toptal",
    "Guru",
    "PeoplePerHour",
    "Direct Client",
    "Other",
  ];

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError(null);

    const amount = parseInt(amountSats, 10);
    if (isNaN(amount) || amount <= 0) {
      setError("Amount must be a positive number of satoshis");
      return;
    }

    if (!payeeInvoice.startsWith("lnbc")) {
      setError("Payee invoice must be a valid Lightning invoice (starts with lnbc)");
      return;
    }

    setIsSubmitting(true);
    try {
      const data: CreateDealRequest = {
        title: title.trim(),
        amount_sats: amount,
        source_platform: sourcePlatform,
        client_email: clientEmail.trim().toLowerCase(),
        payee_invoice: payeeInvoice.trim(),
      };

      const res = await api.createDeal(data, tokens!.access_token);
      router.push(`/deals/${res.deal.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create deal. Try again.");
    } finally {
      setIsSubmitting(false);
    }
  }

  if (!tokens) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center bg-vault-bg px-6">
        <p className="text-ink-500">Redirecting to login...</p>
      </main>
    );
  }

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-3xl items-center justify-between px-6">
          <Link href="/dashboard" className="logo-gnj-sm" aria-label="Ganji Dashboard">
            GNJ
          </Link>
        </div>
      </header>

      <div className="flex-1 flex items-center justify-center px-6 py-12">
        <div className="w-full max-w-2xl">
          <div className="mb-8 text-center">
            <Link href="/dashboard" className="inline-flex items-center gap-2 text-sm text-ink-500 hover:text-ink-300 mb-6 block text-left">
              ← Back to Dashboard
            </Link>
            <h1 className="font-display text-3xl font-semibold text-ink-100 mb-2">Create New Deal</h1>
            <p className="text-ink-500">Set up a Lightning escrow. Funds release when your client approves.</p>
          </div>

          <div className="vault-card p-6 md:p-8 vault-seam">
            <form onSubmit={handleSubmit} className="flex flex-col gap-5" noValidate>
              {error && <FormBanner message={error} />}

              <Input
                label="Deal Title"
                type="text"
                name="title"
                autoComplete="off"
                required
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="e.g., React Dashboard Redesign"
              />

              <Input
                label="Amount (sats)"
                type="number"
                name="amount_sats"
                autoComplete="off"
                required
                value={amountSats}
                onChange={(e) => setAmountSats(e.target.value)}
                placeholder="500000"
                min="1"
              />

              <div>
                <label className="block text-sm font-medium text-ink-300 mb-1.5">Source Platform</label>
                <select
                  name="source_platform"
                  required
                  value={sourcePlatform}
                  onChange={(e) => setSourcePlatform(e.target.value)}
                  className="w-full rounded-lg border border-vault-border bg-vault-raised px-4 py-2.5 text-sm text-ink-100 placeholder-ink-500 focus:border-gold-500 focus:outline-none focus:ring-2 focus:ring-gold-500/30 appearance-none"
                >
                  <option value="">Select platform</option>
                  {platforms.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              </div>

              <Input
                label="Client Email"
                type="email"
                name="client_email"
                autoComplete="email"
                required
                value={clientEmail}
                onChange={(e) => setClientEmail(e.target.value)}
                placeholder="client@company.com"
              />

              <div>
                <label className="block text-sm font-medium text-ink-300 mb-1.5">
                  Your Payee Invoice (Lightning)
                </label>
                <textarea
                  name="payee_invoice"
                  required
                  value={payeeInvoice}
                  onChange={(e) => setPayeeInvoice(e.target.value)}
                  placeholder="lnbc500000u1p3... (your Lightning invoice for receiving payment)"
                  rows={3}
                  className="w-full rounded-lg border border-vault-border bg-vault-raised px-4 py-2.5 text-sm text-ink-100 placeholder-ink-500 focus:border-gold-500 focus:outline-none focus:ring-2 focus:ring-gold-500/30 font-mono text-xs resize-none"
                />
                <p className="mt-1.5 text-xs text-ink-500">
                  Generate a receive invoice from your Lightning wallet (Zap, Phoenix, Breez, etc.)
                </p>
              </div>

              <Button type="submit" isLoading={isSubmitting} size="lg" className="w-full">
                Create Deal
              </Button>
            </form>
          </div>

          <p className="mt-6 text-center text-xs text-ink-700">
            The client will receive a payment link. Funds are held in escrow until they approve delivery.
          </p>
        </div>
      </div>
    </main>
  );
}