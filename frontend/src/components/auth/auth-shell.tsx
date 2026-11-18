"use client";

import { ReactNode } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";

interface AuthShellProps {
  eyebrow: string;
  title: string;
  subtitle: string;
  children: ReactNode;
  footer?: ReactNode;
  showLogo?: boolean;
}

export function AuthShell({ eyebrow, title, subtitle, children, footer }: AuthShellProps) {
  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center gap-8 bg-vault-bg px-6 py-12">
      <div className="absolute inset-x-0 top-0 h-16 border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm">
        <div className="mx-auto flex h-full max-w-7xl items-center px-6">
          <Link href="/" className="brand-wordmark" aria-label="Ganji Home">ganji</Link>
        </div>
      </div>
      <header className="w-full max-w-md">
        <div className="text-center">
          <p className="mb-3 font-mono text-xs uppercase tracking-[0.2em] text-gold-500">{eyebrow}</p>
          <h1 className="mb-3 font-display text-3xl font-semibold text-ink-100">{title}</h1>
          <p className="text-sm text-ink-500">{subtitle}</p>
        </div>
      </header>

      <div className="w-full max-w-md vault-card p-8 vault-seam">
        <div className="flex flex-col gap-6">{children}</div>

        {footer && (
          <div className="mt-6 text-center text-sm text-ink-500">{footer}</div>
        )}
      </div>
    </div>
  );
}
