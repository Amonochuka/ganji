"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { FormBanner } from "@/components/ui/form-banner";
import { api, type CreateDealRequest } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";
import { formatCurrency } from "@/lib/utils/format";

type MilestoneDraft = { title: string; amount_sats: string };

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
  const [milestoneMode, setMilestoneMode] = useState(false);
  const [milestones, setMilestones] = useState<MilestoneDraft[]>([{ title: "Milestone 1", amount_sats: "" }]);

  const platforms = [
    "Upwork", "Fiverr", "Freelancer.com", "Toptal", "Guru", "PeoplePerHour", "Direct Client", "Other",
  ];

  const milestonesTotal = milestones.reduce((sum, m) => {
    const n = /^\d+$/.test(m.amount_sats.trim()) ? parseInt(m.amount_sats, 10) : 0;
    return sum + n;
  }, 0);

  function updateMilestone(i: number, patch: Partial<MilestoneDraft>) {
    setMilestones((prev) => prev.map((m, idx) => (idx === i ? { ...m, ...patch } : m)));
  }
  function addMilestone() {
    setMilestones((prev) => [...prev, { title: `Milestone ${prev.length + 1}`, amount_sats: "" }]);
  }
  function removeMilestone(i: number) {
    setMilestones((prev) => (prev.length <= 1 ? prev : prev.filter((_, idx) => idx !== i)));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError(null);

    if (!title.trim()) return setError("Deal title is required");
    if (!sourcePlatform) return setError("Select the source platform");
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(clientEmail.trim())) return setError("Enter a valid client email");
    if (!payeeInvoice.trim().startsWith("lnbc")) return setError("Payee invoice must be a valid Lightning invoice (starts with lnbc)");

    let amount = 0;
    let msPayload: { title: string; amount_sats: number }[] | undefined;
    if (milestoneMode) {
      const parsed = milestones.map((m, idx) => ({
        title: m.title.trim() || `Milestone ${idx + 1}`,
        amount_sats: /^\d+$/.test(m.amount_sats.trim()) ? parseInt(m.amount_sats, 10) : 0,
      }));
      if (parsed.length === 0) return setError("Add at least one milestone");
      if (parsed.some((m) => !m.title || m.amount_sats <= 0)) return setError("Each milestone needs a title and positive sats amount");
      msPayload = parsed;
      amount = parsed.reduce((s, m) => s + m.amount_sats, 0);
    } else {
      if (!/^\d+$/.test(amountSats.trim()) || parseInt(amountSats, 10) <= 0) return setError("Amount must be a positive whole number of satoshis");
      amount = parseInt(amountSats, 10);
    }

    setIsSubmitting(true);
    try {
      const data: CreateDealRequest = {
        title: title.trim(),
        amount_sats: amount,
        source_platform: sourcePlatform,
        client_email: clientEmail.trim().toLowerCase(),
        payee_invoice: payeeInvoice.trim(),
        ...(msPayload ? { milestones: msPayload } : {}),
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
          <Link href="/dashboard" className="logo-gnj-sm" aria-label="Ganji Dashboard">GNJ</Link>
        </div>
      </header>

      <div className="flex-1 flex items-center justify-center px-6 py-12">
        <div className="w-full max-w-2xl">
          <div className="mb-8 text-center">
            <Link href="/dashboard" className="inline-flex items-center gap-2 text-sm text-ink-500 hover:text-ink-300 mb-6 block text-left">← Back to Dashboard</Link>
            <h1 className="font-display text-3xl font-semibold text-ink-100 mb-2">Create New Deal</h1>
            <p className="text-ink-500">Set up Lightning escrow. Use milestones for sequential per-milestone holds (M1 funded/released, then M2).</p>
          </div>

          <div className="vault-card p-6 md:p-8 vault-seam">
            <form onSubmit={handleSubmit} className="flex flex-col gap-5" noValidate>
              {error && <FormBanner message={error} />}

              <Input label="Deal Title" type="text" name="title" autoComplete="off" required value={title} onChange={(e) => setTitle(e.target.value)} placeholder="e.g., React Dashboard Redesign" />

              <div className="rounded-lg border border-vault-border p-4 space-y-3">
                <label className="flex items-center gap-2 text-sm text-ink-200">
                  <input type="checkbox" checked={milestoneMode} onChange={(e) => setMilestoneMode(e.target.checked)} />
                  Split into sequential milestones (per-milestone escrow holds)
                </label>
                {!milestoneMode ? (
                  <Input label="Amount (sats)" type="number" name="amount_sats" autoComplete="off" required value={amountSats} onChange={(e) => setAmountSats(e.target.value)} placeholder="500000" min="1" />
                ) : (
                  <div className="space-y-3">
                    <p className="text-xs text-ink-500">Client funds M1 first; after release, M2 is funded. Total: <span className="font-mono text-ink-200">{formatCurrency(milestonesTotal)}</span></p>
                    {milestones.map((m, i) => (
                      <div key={i} className="grid grid-cols-1 md:grid-cols-6 gap-2 items-end">
                        <div className="md:col-span-3">
                          <Input label={i === 0 ? "Milestone title" : `Milestone ${i + 1} title`} type="text" value={m.title} onChange={(e) => updateMilestone(i, { title: e.target.value })} placeholder={`Milestone ${i + 1}`} />
                        </div>
                        <div className="md:col-span-2">
                          <Input label={i === 0 ? "Amount (sats)" : `Milestone ${i + 1} sats`} type="number" value={m.amount_sats} onChange={(e) => updateMilestone(i, { amount_sats: e.target.value })} placeholder="250000" min="1" />
                        </div>
                        <div className="md:col-span-1">
                          <Button type="button" variant="ghost" size="sm" onClick={() => removeMilestone(i)} disabled={milestones.length <= 1}>Remove</Button>
                        </div>
                      </div>
                    ))}
                    <Button type="button" variant="outline" size="sm" onClick={addMilestone}>Add milestone</Button>
                  </div>
                )}
              </div>

              <div>
                <label className="block text-sm font-medium text-ink-300 mb-1.5">Source Platform</label>
                <select name="source_platform" required value={sourcePlatform} onChange={(e) => setSourcePlatform(e.target.value)} className="w-full rounded-lg border border-vault-border bg-vault-raised px-4 py-2.5 text-sm text-ink-100 placeholder-ink-500 focus:border-gold-500 focus:outline-none focus:ring-2 focus:ring-gold-500/30 appearance-none">
                  <option value="">Select platform</option>
                  {platforms.map((p) => (<option key={p} value={p}>{p}</option>))}
                </select>
              </div>

              <Input label="Client Email" type="email" name="client_email" autoComplete="email" required value={clientEmail} onChange={(e) => setClientEmail(e.target.value)} placeholder="client@company.com" />

              <div>
                <label className="block text-sm font-medium text-ink-300 mb-1.5">Your Payee Invoice (Lightning)</label>
                <textarea name="payee_invoice" required value={payeeInvoice} onChange={(e) => setPayeeInvoice(e.target.value)} placeholder="lnbc500000u1p3... (your Lightning invoice for receiving payment)" rows={3} className="w-full rounded-lg border border-vault-border bg-vault-raised px-4 py-2.5 text-sm text-ink-100 placeholder-ink-500 focus:border-gold-500 focus:outline-none focus:ring-2 focus:ring-gold-500/30 font-mono text-xs resize-none" />
                <p className="mt-1.5 text-xs text-ink-500">Generate a receive invoice from your Lightning wallet (Zap, Phoenix, Breez, etc.)</p>
              </div>

              <Button type="submit" isLoading={isSubmitting} size="lg" className="w-full">Create Deal</Button>
            </form>
          </div>

          <p className="mt-6 text-center text-xs text-ink-700">In milestone mode, M1 gets a hold invoice after creation (client pays). Later milestones are funded sequentially.</p>
        </div>
      </div>
    </main>
  );
}
