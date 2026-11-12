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

type FieldErrors = {
  password?: string;
  confirmPassword?: string;
};

export default function SignupPage() {
  const router = useRouter();
  const { signup } = useAuth();

  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [showConfirmPassword, setShowConfirmPassword] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  function validate(): boolean {
    const errors: FieldErrors = {};
    if (password.length < 8) errors.password = "Must be at least 8 characters.";
    if (confirmPassword !== password) errors.confirmPassword = "Passwords don't match.";
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError(null);
    if (!validate()) return;

    setIsSubmitting(true);
    try {
      await signup({ email, password, display_name: displayName });
      router.push("/dashboard");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong. Try again.");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthShell
      eyebrow="Get started"
      title="Create your account"
      subtitle="Your Live CV starts the moment your first deal closes."
      footer={
        <>
          Already have an account?{" "}
          <Link href="/login" className="font-medium text-gold-500 hover:text-gold-400 underline underline-offset-2">
            Log in
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit} className="flex flex-col gap-5" noValidate>
        {error && <FormBanner message={error} />}
        <Input
          label="Display name"
          type="text"
          name="display_name"
          autoComplete="name"
          required
          value={displayName}
          onChange={(event) => setDisplayName(event.target.value)}
        />
        <Input
          label="Email"
          type="email"
          name="email"
          autoComplete="off"
          required
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
        <Input
          label="Password"
          type={showPassword ? "text" : "password"}
          name="password"
          autoComplete="new-password"
          required
          value={password}
          error={fieldErrors.password}
          onChange={(event) => setPassword(event.target.value)}
        />
        <button
          type="button"
          className="absolute right-3 top-1/2 -translate-y-1/2 p-1 rounded-md hover:bg-gray-100 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:pointer-events-none disabled:opacity-50"
          onClick={e => {
            e.stopPropagation()
            setShowPassword(prev => !prev)
          }}
          aria-label={showPassword ? "Hide password" : "Show password"}
        >
          <svg
            className="h-4 w-4 text-gray-400"
            viewBox="0 0 20 20"
            fill="currentColor"
          >
            <path
              d="M13.586 8.414a2 2 0 1 1 2.828 2.828l-1.414 1.414-3.066 3.066 1.414 1.414L9 12.828l6.293-6.293a2 2 0 0 1 0 2.828z"
            />
            <path
              d="M1 4.293a2 2 0 0 1 2.828-2.828l8.596 8.596a2 2 0 0 1-2.828 2.828L11 8.828H3a2 2 0 0 1-2-2V4.293z"
            />
          </svg>
        </button>
        <Input
          label="Confirm password"
          type={showConfirmPassword ? "text" : "password"}
          name="confirm_password"
          autoComplete="new-password"
          required
          value={confirmPassword}
          error={fieldErrors.confirmPassword}
          onChange={(event) => setConfirmPassword(event.target.value)}
        />
        <button
          type="button"
          className="absolute right-3 top-1/2 -translate-y-1/2 p-1 rounded-md hover:bg-gray-100 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:pointer-events-none disabled:opacity-50"
          onClick={e => {
            e.stopPropagation()
            setShowConfirmPassword(prev => !prev)
          }}
          aria-label={showConfirmPassword ? "Hide password" : "Show password"}
        >
          <svg
            className="h-4 w-4 text-gray-400"
            viewBox="0 0 20 20"
            fill="currentColor"
          >
            <path
              d="M13.586 8.414a2 2 0 1 1 2.828 2.828l-1.414 1.414-3.066 3.066 1.414 1.414L9 12.828l6.293-6.293a2 2 0 0 1 0 2.828z"
            />
            <path
              d="M1 4.293a2 2 0 0 1 2.828-2.828l8.596 8.596a2 2 0 0 1-2.828 2.828L11 8.828H3a2 2 0 0 1-2-2V4.293z"
            />
          </svg>
        </button>
        <Button type="submit" isLoading={isSubmitting} size="lg">
          Create account
        </Button>
      </form>
    </AuthShell>
  );
}