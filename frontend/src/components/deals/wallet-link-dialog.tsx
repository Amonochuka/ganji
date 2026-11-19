"use client";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";

interface WalletLinkDialogProps {
  open: boolean;
  title: string;
  walletURL: string;
  walletKey: string;
  keyLabel: string;
  keyPlaceholder: string;
  submitLabel: string;
  error?: string | null;
  isSubmitting?: boolean;
  onClose: () => void;
  onWalletURLChange: (value: string) => void;
  onWalletKeyChange: (value: string) => void;
  onSubmit: () => void;
}

export function WalletLinkDialog({
  open,
  title,
  walletURL,
  walletKey,
  keyLabel,
  keyPlaceholder,
  submitLabel,
  error,
  isSubmitting = false,
  onClose,
  onWalletURLChange,
  onWalletKeyChange,
  onSubmit,
}: WalletLinkDialogProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-6" onMouseDown={onClose}>
      <div
        className="vault-card w-full max-w-md p-6 vault-seam"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <h2 className="font-display text-xl font-semibold text-ink-100 mb-2">{title}</h2>
        <p className="text-xs text-ink-500 mb-5">
          Enter your LNbits wallet URL and {keyLabel.toLowerCase()} from your LNbits API keys screen — not a wallet ID, Lightning address, or exchange login. Ganji uses them only for this request and does not store them.
        </p>
        <div className="space-y-4">
          <Input
            label="Wallet URL"
            type="url"
            value={walletURL}
            onChange={(event) => onWalletURLChange(event.target.value)}
            placeholder="http://localhost:5000"
          />
          <PasswordInput
            label={keyLabel}
            value={walletKey}
            onChange={(event) => onWalletKeyChange(event.target.value)}
            placeholder={keyPlaceholder}
            autoComplete="off"
          />
        </div>
        {error && <p className="mt-3 text-sm text-red-400">{error}</p>}
        <div className="mt-6 flex gap-2">
          <Button variant="ghost" className="flex-1" onClick={onClose} disabled={isSubmitting}>Cancel</Button>
          <Button className="flex-1" onClick={onSubmit} isLoading={isSubmitting} disabled={isSubmitting}>{submitLabel}</Button>
        </div>
      </div>
    </div>
  );
}
