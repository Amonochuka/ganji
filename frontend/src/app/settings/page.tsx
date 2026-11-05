"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { api, type User } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";

export default function SettingsPage() {
  const router = useRouter();
  const { tokens, user: ctxUser, logout } = useAuth();
  const [user, setUser] = useState<User | null>(ctxUser);
  const [displayName, setDisplayName] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [defaultPayeeInvoice, setDefaultPayeeInvoice] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [saving, setSaving] = useState(false);
  const [success, setSuccess] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tokens) {
      router.push("/login");
      return;
    }
    ;(async () => {
      try {
        const res = await api.getMe(tokens.access_token);
        setUser(res.user);
        setDisplayName(res.user.display_name);
        setDefaultPayeeInvoice(res.user.default_payee_invoice || "");
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load profile");
      } finally {
        setIsLoading(false);
      }
    })();
  }, [tokens, router]);

  async function handleSave() {
    if (!tokens) return;
    setSaving(true);
    setError(null);
    setSuccess(false);
    try {
      if (!user) return;
      await api.updatePayeeInvoice(user.id, { payee_invoice: defaultPayeeInvoice }, tokens.access_token);
      if (newPassword.trim()) {
        await fetch(`${process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080"}/auth/me`, {
          method: "PATCH",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${tokens.access_token}`,
          },
          body: JSON.stringify({ new_password: newPassword }),
        });
      }
      setUser((prev) => {
        if (!prev) return prev;
        return {
          ...prev,
          display_name: displayName,
          default_payee_invoice: defaultPayeeInvoice,
        };
      });
      setSuccess(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  }

  if (!tokens) return null;

  return (
    <main className="flex min-h-screen flex-col bg-vault-bg">
      <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
          <Link href="/dashboard" className="logo-gnj-sm" aria-label="Ganji Dashboard">GNJ</Link>
          <nav className="flex items-center gap-4">
            <span className="text-sm text-ink-300 hidden sm:block">{user?.display_name}</span>
            <Button variant="ghost" className="h-10 px-4" onClick={() => logout()}>Log out</Button>
          </nav>
        </div>
      </header>
      <div className="flex-1 px-6 py-10 md:py-16">
        <div className="mx-auto max-w-2xl">
          <Link href="/dashboard" className="inline-flex items-center gap-2 text-sm text-ink-500 hover:text-ink-300 mb-6 block">
            ← Dashboard
          </Link>
          <h1 className="font-display text-3xl font-semibold text-ink-100 mb-6">Settings</h1>

          {isLoading && (
            <div className="flex items-center justify-center py-12">
              <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
            </div>
          )}

          {error && <div className="vault-card p-6 border-red-600/30 text-red-400 mb-6">{error}</div>}

          {user && (
            <div className="vault-card p-6 vault-seam space-y-4">
              <h2 className="font-display text-lg font-semibold text-ink-100">Profile</h2>

              <div className="space-y-3">
                <label className="block text-sm text-ink-300">
                  Display name
                  <input
                    value={user.display_name}
                    onChange={(e) => setDisplayName(e.target.value)}
                    className="w-full rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm text-ink-100"
                  />
                </label>
                <p className="text-xs text-ink-500">Your display name appears on your CV and public profiles</p>
              </div>

              <div className="space-y-3">
                <label className="block text-sm text-ink-300">
                  Default payee invoice
                  <input
                    value={user.default_payee_invoice || ""}
                    onChange={(e) => setDefaultPayeeInvoice(e.target.value)}
                    className="w-full rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm font-mono text-ink-300"
                  />
                  <p className="text-xs text-ink-500">Invoice where payouts are sent on approve</p>
                </label>
              </div>

              <div className="space-y-3">
                <label className="block text-sm text-ink-300">
                  New password (leave blank to keep current)
                  <input
                    type="password"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    className="w-full rounded-lg border border-vault-border bg-vault-raised px-3 py-2 text-sm text-ink-300"
                  />
                  <p className="text-xs text-ink-500">Optional: change your password</p>
                </label>
              </div>

              <Button
                variant="primary"
                onClick={handleSave}
                disabled={saving}
                isLoading={saving}
              >
                {saving ? "Saving..." : "Save Changes"}
              </Button>
            </div>
          )}

          {saving && (
            <div className="flex items-center justify-center py-8">
              <div className="h-8 w-8 animate-spin rounded-full border-2 border-gold-500 border-t-transparent" />
              <span className="ml-4 text-sm text-ink-300">Saving...</span>
            </div>
          )}

          {success && (
            <div className="vault-card p-6 border-green-600/30 text-green-400 mb-6">
              Changes saved successfully.
            </div>
          )}
        </div>
      </div>
    </main>
  );
}