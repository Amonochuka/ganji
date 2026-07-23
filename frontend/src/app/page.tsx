"use client";

import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/lib/auth/auth-context";

export default function Home() {
  const { user, isLoading, logout } = useAuth();

  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-6 bg-vault-bg px-6 text-center">
      <p className="font-mono text-xs uppercase tracking-[0.2em] text-sats-500">Ganji</p>
      <h1 className="max-w-md font-display text-3xl font-semibold text-ink-100">
        Lightning escrow, without the platform.
      </h1>
      <p className="max-w-sm text-sm text-ink-500">
        Funds locked until the client approves. Every deal becomes a hash-verified line on your Live CV.
      </p>

      {isLoading ? (
        <p className="text-sm text-ink-500">Loading…</p>
      ) : user ? (
        <div className="flex flex-col items-center gap-3">
          <p className="text-sm text-ink-300">
            Signed in as <span className="text-ink-100">{user.display_name}</span>
          </p>
          <Button variant="ghost" className="w-auto px-6" onClick={() => logout()}>
            Log out
          </Button>
        </div>
      ) : (
        <div className="flex gap-3">
          <Link href="/login">
            <Button variant="ghost" className="w-auto px-6">
              Log in
            </Button>
          </Link>
          <Link href="/signup">
            <Button className="w-auto px-6">Sign up</Button>
          </Link>
        </div>
      )}
    </main>
  );
}
