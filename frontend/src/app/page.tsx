"use client";

import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/lib/auth/auth-context";
import { useRouter } from "next/navigation";

export default function Home() {
  const { user, isLoading, logout } = useAuth();
  const router = useRouter();

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-6">
          <Link href="/" className="brand-wordmark" aria-label="Ganji Home">
            ganji
          </Link>
          <button
            className="md:hidden flex items-center gap-2 p-2 rounded-lg bg-vault-raised text-ink-300 hover:bg-vault-border"
            onClick={() => router.push("/")}
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

      <section className="flex-1 flex flex-col items-center justify-center gap-12 px-6 py-20 md:py-32">
        <div className="mx-auto max-w-4xl text-center">
          <div className="mb-8 flex items-center justify-center gap-3">
            <span className="logo-gnj">ganji</span>
          </div>

          <h1 className="mb-6 font-display text-4xl md:text-5xl lg:text-6xl font-semibold hero-gradient leading-tight">
            You do the work.<br />
            <span className="text-gold-400">Lightning handles the trust.</span>
          </h1>

          <div className="mt-6 mb-8 flex flex-col items-center justify-center gap-2">
            <div className="w-full max-w-32 h-1 bg-vault-border border-t border-vault-border rounded-t-lg rounded-b-lg" />
            <span className="text-xs text-ink-500 uppercase tracking-widest">Powered by Bitcoin Lightning</span>
          </div>

          <div className="flex flex-col items-center justify-center gap-4">
            <Link href="/signup">
              <Button size="lg" className="w-full py-3.5 px-8 text-base">
                Start Escrowing
              </Button>
            </Link>
            <Link href="/login">
              <Button variant="outline" size="lg" className="w-full py-3.5 px-8 text-base">
                Log In
              </Button>
            </Link>
          </div>

          <EscrowFlow />
        </div>

        <div className="mt-8 w-full max-w-4xl">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <FeatureCard
              icon={<path d="M7 10V7a5 5 0 0 1 10 0v3m-11 0h12v10H6V10Zm6 4v2" />}
              title="Protection for Both Sides"
              description="The client funds escrow before work begins. The freelancer gets paid when work is approved; disputes keep funds held for review."
            />
            <FeatureCard
              icon={<><path d="M9 15H7a5 5 0 0 1 0-10h2" /><path d="M15 9h2a5 5 0 0 1 0 10h-2" /><path d="M8 10h8" /></>}
              title="Verifiable Work History"
              description="Completed deals can add a timestamped proof to a freelancer’s Live CV, helping clients verify their track record."
            />
            <FeatureCard
              icon={<path d="m13 2-8 12h6l-1 8 8-12h-6l1-8Z" />}
              title="Lightning Native"
              description="Pay and receive bitcoin over Lightning, with fast settlement and no bank account required."
            />
          </div>
        </div>
      </section>

      <footer className="border-t border-vault-border bg-vault-surface py-12 px-6">
        <div className="mx-auto max-w-7xl text-center">
          <p className="text-sm text-ink-500">
            Built for freelancers. Powered by Bitcoin. No platform lock-in.
          </p>
          <p className="mt-2 text-xs text-ink-700 font-mono">
            GNJ — Lightning Escrow Protocol
          </p>
        </div>
      </footer>
    </main>
  );
}

function EscrowFlow() {
  return (
    <section aria-label="How Ganji escrow works" className="mx-auto mt-16 w-full max-w-4xl">
      <p className="mb-6 text-center text-[11px] font-medium uppercase tracking-[0.22em] text-ink-500">
        A simple, protected flow
      </p>
      <div className="relative grid grid-cols-1 gap-5 sm:grid-cols-5 sm:gap-2">
        <div className="absolute bottom-10 left-6 top-6 w-px bg-gradient-to-b from-vault-border via-gold-500/50 to-vault-border sm:bottom-auto sm:left-[10%] sm:right-[10%] sm:top-6 sm:h-px sm:w-auto sm:bg-gradient-to-r" aria-hidden="true" />
        <FlowStep
          label="Invoice ready"
          detail="Freelancer shares a payout invoice"
          icon={<path d="m13 2-8 12h6l-1 8 8-12h-6l1-8Z" />}
        />
        <FlowStep
          label="Client funds"
          detail="Pays the Lightning hold"
          icon={<><path d="M3 7h18v12H3V7Z" /><path d="M3 11h18" /><path d="M16 15h2" /></>}
        />
        <FlowStep
          label="Work delivered"
          detail="Funds stay held while work is reviewed"
          featured
          icon={<><path d="M7 10V7a5 5 0 0 1 10 0v3m-11 0h12v10H6V10Z" /><path d="m9 15 2 2 4-4" /></>}
        />
        <FlowStep
          label="Client approves"
          detail="Escrow pays the freelancer"
          icon={<path d="m5 12 4 4L19 6" />}
        />
        <FlowStep
          label="Client rejects"
          detail="Funds stay held for independent review"
          alternate
          icon={<path d="m7 7 10 10M17 7 7 17" />}
        />
      </div>
      <p className="mt-5 text-center text-xs text-ink-500">
        Independent review resolves a rejection: funds are released to the freelancer or refunded to the client.
      </p>
    </section>
  );
}

function FlowStep({ label, detail, icon, featured = false, alternate = false }: {
  label: string;
  detail: string;
  icon: React.ReactNode;
  featured?: boolean;
  alternate?: boolean;
}) {
  return (
    <div className="relative z-10 flex items-center gap-4 bg-vault-bg sm:flex-col sm:gap-0">
      <div className={`flex h-12 w-12 shrink-0 items-center justify-center rounded-full border bg-vault-bg sm:mb-3 ${featured ? "border-gold-500/60 text-gold-400 shadow-[0_0_24px_rgba(196,150,50,0.16)]" : alternate ? "border-amber-700/50 text-amber-300" : "border-vault-border text-ink-300"}`}>
        <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          {icon}
        </svg>
      </div>
      <div className="text-left sm:text-center">
        <p className={`text-sm font-medium ${featured ? "text-gold-400" : alternate ? "text-amber-300" : "text-ink-100"}`}>{label}</p>
        <p className="mt-1 text-xs leading-relaxed text-ink-500">{detail}</p>
      </div>
    </div>
  );
}

function FeatureCard({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <div
      className={`vault-card p-6 text-center cursor-pointer transition-all hover:border-gold-500/30 hover:shadow-[0_0_0_1px_rgba(196,150,50,0.15)]`}
    >
      <div className="mx-auto mb-4 flex h-10 w-10 items-center justify-center text-gold-400">
        <svg className="h-7 w-7" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          {icon}
        </svg>
      </div>
      <h3 className="mb-2 font-display text-lg font-semibold text-ink-100">{title}</h3>
      <p className="text-sm text-ink-500 leading-relaxed">{description}</p>
    </div>
  );
}
