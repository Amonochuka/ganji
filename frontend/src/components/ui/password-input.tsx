"use client";

import { forwardRef, useState, type InputHTMLAttributes } from "react";
import { Input } from "@/components/ui/input";

interface PasswordInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "type"> {
  label: string;
  error?: string;
}

export const PasswordInput = forwardRef<HTMLInputElement, PasswordInputProps>(
  ({ label, error, className = "", ...props }, ref) => {
    const [isVisible, setIsVisible] = useState(false);

    return (
      <div className="relative">
        <Input
          ref={ref}
          label={label}
          error={error}
          type={isVisible ? "text" : "password"}
          className={`pr-11 ${className}`}
          {...props}
        />
        <button
          type="button"
          className="absolute right-3 top-[3rem] -translate-y-1/2 rounded-md p-1.5 text-ink-400 transition hover:bg-vault-raised hover:text-ink-100 focus:outline-none focus:ring-2 focus:ring-gold-500 focus:ring-offset-2 focus:ring-offset-vault-bg disabled:pointer-events-none disabled:opacity-50"
          onClick={() => setIsVisible((visible) => !visible)}
          aria-label={isVisible ? "Hide password" : "Show password"}
          aria-pressed={isVisible}
        >
          {isVisible ? (
            <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M3.3 1.3a1 1 0 0 0-1.4 1.4l14 14a1 1 0 0 0 1.4-1.4L3.3 1.3Z" />
              <path d="M10 4c4.05 0 7.45 2.42 9 6a10.6 10.6 0 0 1-3.1 3.85l-1.42-1.42A8.5 8.5 0 0 0 17.09 10c-1.42-2.78-4.08-4.5-7.09-4.5-.9 0-1.75.14-2.53.4L5.64 4.08A9.86 9.86 0 0 1 10 4Z" />
              <path d="M1 10c1.55-4.08 5.2-6.5 9-6.5.94 0 1.84.13 2.68.36L11.4 5.13A5.6 5.6 0 0 0 10 5c-3.01 0-5.67 1.72-7.09 4.5.5.98 1.2 1.86 2.04 2.6L3.53 13.5A10.56 10.56 0 0 1 1 10Z" />
              <path d="M10 7.5a2.5 2.5 0 0 1 2.45 3.05L9.45 7.55A2.49 2.49 0 0 1 10 7.5Z" />
              <path d="M10 12.5a2.5 2.5 0 0 1-2.45-3.05l3 3A2.49 2.49 0 0 1 10 12.5Z" />
            </svg>
          ) : (
            <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M10 3.5c4.05 0 7.45 2.42 9 6.5-1.55 4.08-5.95 6.5-9 6.5S2.55 14.08 1 10c1.55-4.08 5.95-6.5 9-6.5Zm0 2a4.5 4.5 0 1 0 0 9 4.5 4.5 0 0 0 0-9Zm0 2a2.5 2.5 0 1 1 0 5 2.5 2.5 0 0 1 0-5Z" />
            </svg>
          )}
        </button>
      </div>
    );
  }
);

PasswordInput.displayName = "PasswordInput";
