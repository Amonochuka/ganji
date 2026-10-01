"use client";

import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/lib/auth/auth-context";

export default function Home() {
  const { user, isLoading, logout } = useAuth();

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-6">
          <Link href="/" className="logo-gnj-sm" aria-label="Ganji Home">
            GNJ
          </Link>
          <nav className="flex items-center gap-4">
            {isLoading ? (
              <div className="h-10 w-24 animate-pulse rounded-lg bg-vault-raised" />
            ) : user ? (
              <div className="flex items-center gap-3">
                <span className="text-sm text-ink-300 hidden sm:block">
                  {user.display_name}
                </span>
                <Button variant="ghost" className="h-10 px-4" onClick={() => logout()}>
                  Log out
                </Button>
              </div>
            ) : (
              <div className="flex items-center gap-3">
                <Link href="/login">
                  <Button variant="ghost" className="h-10 px-4">
                    Log in
                  </Button>
                </Link>
                <Link href="/signup">
                  <Button className="h-10 px-6">Get Started</Button>
                </Link>
              </div>
            )}
          </nav>
        </div>
      </header>

      <section className="flex-1 flex flex-col items-center justify-center gap-12 px-6 py-20 md:py-32">
        <div className="mx-auto max-w-4xl text-center">
          <div className="mb-8 flex items-center justify-center gap-3">
            <span className="logo-gnj gold-glow">GNJ</span>
          </div>

          <h1 className="mb-6 font-display text-4xl md:text-5xl lg:text-6xl font-semibold hero-gradient leading-tight">
            You do the work.<br />
            <span className="text-gold-400">Lightning handles the trust.</span>
          </h1>

          <p className="mx-auto mb-10 max-w-2xl text-lg md:text-xl text-ink-300 leading-relaxed">
            Funds locked in escrow until the client approves. Zero platform risk. Every closed deal
            anchors a cryptographic proof to Bitcoin — your reputation, verifiable by anyone.
          </p>

          <div className="flex flex-col sm:flex-row items-center justify-center gap-4">
            <Link href="/signup">
              <Button size="lg" className="w-full sm:w-auto min-w-[200px] py-3.5 px-8 text-base">
                Start Escrowing
              </Button>
            </Link>
            <Link href="/login">
              <Button variant="outline" size="lg" className="w-full sm:w-auto min-w-[200px] py-3.5 px-8 text-base">
                Log In
              </Button>
            </Link>
          </div>
        </div>

        <div className="mt-8 w-full max-w-4xl">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <FeatureCard
              icon="🔒"
              title="Client-Approved Release"
              description="Funds stay locked in Lightning escrow until the client explicitly approves delivery. No disputes, no chargebacks."
            />
            <FeatureCard
              icon="⛓️"
              title="Hash-Verified Reputation"
              description="Every completed deal anchors a cryptographic proof to Bitcoin via OpenTimestamps. Your Live CV is immutable."
            />
            <FeatureCard
              icon="⚡"
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

function FeatureCard({
  icon,
  title,
  description,
}: {
  icon: string;
  title: string;
  description: string;
}) {
  return (
    <div className="vault-card p-6 text-center">
      <div className="mb-4 text-4xl">{icon}</div>
      <h3 className="mb-2 font-display text-lg font-semibold text-ink-100">{title}</h3>
      <p className="text-sm text-ink-500 leading-relaxed">{description}</p>
    </div>
  );
}