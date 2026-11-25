"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { Button } from "@/components/ui/button";
import { FormBanner } from "@/components/ui/form-banner";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
import { ApiError } from "@/lib/api/api-client";
import { useAuth } from "@/lib/auth/auth-context";

type ViewRole = "client" | "freelancer";

function safeNextPath(value: string | null): string | null {
  if (!value || !value.startsWith("/") || value.startsWith("//")) return null;
  return value;
}

function safeRole(value: string | null): ViewRole | null {
  return value === "client" || value === "freelancer" ? value : null;
}

function withRole(path: string, role: ViewRole | null): string {
  if (!role) return path;
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}role=${role}`;
}

export default function LoginPage() {
  const router = useRouter();
  const { login } = useAuth();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [nextPath, setNextPath] = useState<string | null>(null);
  const [role, setRole] = useState<ViewRole | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    setNextPath(safeNextPath(params.get("next")));
    setRole(safeRole(params.get("role")));
    if (params.has("email") || params.has("password") || params.has("next") || params.has("role")) {
      window.history.replaceState({}, "", window.location.pathname);
    }
  }, []);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError(null);
    setIsSubmitting(true);
    try {
      await login({ email, password });
      router.push(withRole(nextPath || "/dashboard", role));
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
      subtitle={role === "client" ? "Review and approve work sent to your client email." : "Pick up your deals where you left them."}
      footer={
        <>
          Don&apos;t have an account?{" "}
          <Link href={withRole(nextPath ? `/signup?next=${encodeURIComponent(nextPath)}` : "/signup", role)} className="font-medium text-gold-500 hover:text-gold-400 underline underline-offset-2">
            Create one
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit} method="post" className="flex flex-col gap-5" noValidate>
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
        <PasswordInput
          label="Password"
          name="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
        <Button type="submit" isLoading={isSubmitting} size="lg">
          Log in
        </Button>
      </form>
    </AuthShell>
  );
}
