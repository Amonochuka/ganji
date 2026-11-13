"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { Button } from "@/components/ui/button";
import { FormBanner } from "@/components/ui/form-banner";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";

export default function LoginPage() {
  const router = useRouter();
  const { login } = useAuth();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError(null);
    setIsSubmitting(true);
    try {
      await login({ email, password });
      router.push("/dashboard");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong. Try again.");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthShell
      eyebrow="Welcome back"
      title="Log in to Ganji"
      subtitle="Pick up your deals where you left them."
      showLogo={false}
      footer={
        <>
          Don&apos;t have an account?{" "}
          <Link href="/signup" className="font-medium text-gold-500 hover:text-gold-400 underline underline-offset-2">
            Create one
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit} className="flex flex-col gap-5" noValidate>
        {error && <FormBanner message={error} />}
        <Input
          label="Email"
          type="email"
          name="email"
          autoComplete="email"
          required
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
        <div className="relative">
          <Input
            label="Password"
            type={showPassword ? "text" : "password"}
            name="password"
            autoComplete="current-password"
            required
            value={password}
            className="pr-10"
            onChange={(event) => setPassword(event.target.value)}
          />
          <button
            type="button"
            className="absolute right-3 top-[2.7rem] -translate-y-1/2 rounded-md p-1.5 text-ink-400 transition hover:bg-vault-raised hover:text-ink-100 focus:outline-none focus:ring-2 focus:ring-gold-500 focus:ring-offset-2 focus:ring-offset-vault-bg disabled:pointer-events-none disabled:opacity-50"
            onClick={e => {
              e.stopPropagation();
              setShowPassword(prev => !prev);
            }}
            aria-label={showPassword ? "Hide password" : "Show password"}
          >
            <svg
              className="h-4 w-4"
              viewBox="0 0 20 20"
              fill="currentColor"
              aria-hidden="true"
            >
              <path d="M10 3.5c4.05 0 7.45 2.42 9 6.5-1.55 4.08-5.95 6.5-9 6.5S2.55 14.08 1 10c1.55-4.08 5.95-6.5 9-6.5Zm0 2a4.5 4.5 0 1 0 0 9 4.5 4.5 0 0 0 0-9Zm0 2a2.5 2.5 0 1 1 0 5 2.5 2.5 0 0 1 0-5Z" />
            </svg>
          </button>
        </div>
        <Button type="submit" isLoading={isSubmitting} size="lg">
          Log in
        </Button>
      </form>
    </AuthShell>
  );
}