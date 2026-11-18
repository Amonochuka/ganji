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

          <p className="mx-auto mb-10 max-w-2xl text-lg md:text-xl text-ink-300 leading-relaxed">
            Freelancers generate a payout invoice. Clients fund a Lightning hold from their linked wallet.
            Ganji marks funds held only after the escrow node accepts that hold — then approval reveals
            the secret and pays the freelancer directly.
          </p>

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
              title="Client-Approved Release"
              description="Funds stay held on Lightning until the client explicitly approves delivery. No disputes, no chargebacks."
            />
            <FeatureCard
              icon={<><path d="M9 15H7a5 5 0 0 1 0-10h2" /><path d="M15 9h2a5 5 0 0 1 0 10h-2" /><path d="M8 10h8" /></>}
              title="Hash-Verified Reputation"
              description="Every completed deal anchors a cryptographic proof to Bitcoin via OpenTimestamps. Your Live CV is immutable."
            />
            <FeatureCard
              icon={<path d="m13 2-8 12h6l-1 8 8-12h-6l1-8Z" />}
              title="Lightning Native"
              description="Built on Bitcoin Lightning. Instant settlements, sub-cent fees, no bank account required. Global from day one."
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
        Full Lightning flow
      </p>
      <div className="grid grid-cols-2 gap-6 sm:grid-cols-4">
        <FlowStep
          label="Payout invoice"
          detail="Freelancer generates the lnbc invoice"
          icon={<path d="m13 2-8 12h6l-1 8 8-12h-6l1-8Z" />}
        />
        <FlowStep
          label="Client funds"
          detail="Linked wallet pays the hold invoice"
          icon={<><path d="M3 7h18v12H3V7Z" /><path d="M3 11h18" /><path d="M16 15h2" /></>}
        />
        <FlowStep
          label="Funds Held"
          detail="Escrow node accepts the hold"
          featured
          icon={<path d="M7 10V7a5 5 0 0 1 10 0v3m-11 0h12v10H6V10Zm6 4v2" />}
        />
        <FlowStep
          label="Approve & pay"
          detail="R is revealed; freelancer is paid"
          icon={<path d="m5 12 4 4L19 6" />}
        />
      </div>
    </section>
  );
}

function FlowStep({ label, detail, icon, featured = false }: {
  label: string;
  detail: string;
  icon: React.ReactNode;
  featured?: boolean;
}) {
  return (
    <div className="relative z-10 flex flex-col items-center text-center">
      <div className={`mb-3 flex h-12 w-12 items-center justify-center rounded-full border bg-vault-bg ${featured ? "border-gold-500/60 text-gold-400 shadow-[0_0_24px_rgba(196,150,50,0.16)]" : "border-vault-border text-ink-300"}`}>
        <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          {icon}
        </svg>
      </div>
      <p className={`text-sm font-medium ${featured ? "text-gold-400" : "text-ink-100"}`}>{label}</p>
      <p className="mt-1 text-xs leading-relaxed text-ink-500">{detail}</p>
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
