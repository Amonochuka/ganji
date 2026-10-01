"use client";

import { DealView, DealStatus } from "@/lib/api/api-client";
import { formatCurrency } from "@/lib/utils/format";

interface DealCardProps {
  deal: DealView;
  onClick?: () => void;
}

const statusConfig: Record<DealStatus, { label: string; className: string; icon: string }> = {
  awaiting_payment: { label: "Awaiting Payment", className: "bg-yellow-600/20 text-yellow-400 border-yellow-600/30", icon: "⏳" },
  locked: { label: "Locked", className: "bg-blue-600/20 text-blue-400 border-blue-600/30", icon: "🔒" },
  work_submitted: { label: "Work Submitted", className: "bg-purple-600/20 text-purple-400 border-purple-600/30", icon: "📤" },
  reviewing: { label: "Under Review", className: "bg-indigo-600/20 text-indigo-400 border-indigo-600/30", icon: "👁️" },
  released: { label: "Released", className: "bg-green-600/20 text-green-400 border-green-600/30", icon: "✅" },
  disputed: { label: "Disputed", className: "bg-red-600/20 text-red-400 border-red-600/30", icon: "⚠️" },
  refunded: { label: "Refunded", className: "bg-gray-600/20 text-gray-400 border-gray-600/30", icon: "↩️" },
};

export function DealCard({ deal, onClick }: DealCardProps) {
  const config = statusConfig[deal.status];
  const amount = formatCurrency(deal.amount_sats);

  return (
    <article
      className={`vault-card p-6 cursor-pointer transition-all hover:border-gold-500/50 ${onClick ? "hover:shadow-[0_0_0_1px_rgba(196,150,50,0.2)]" : ""}`}
      onClick={onClick}
    >
      <div className="flex items-start justify-between gap-4 mb-4">
        <div className="flex-1 min-w-0">
          <h3 className="font-display text-lg font-semibold text-ink-100 truncate">{deal.title}</h3>
          <p className="mt-1 text-sm text-ink-500 font-mono">{deal.source_platform}</p>
        </div>
        <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border ${config.className} whitespace-nowrap`}>
          {config.icon} {config.label}
        </span>
      </div>

      <div className="grid grid-cols-2 gap-4 mb-4">
        <div>
          <p className="text-xs text-ink-500 uppercase tracking-wide">Amount</p>
          <p className="font-mono text-xl font-medium text-ink-100 mono-tabular">{amount}</p>
        </div>
        <div>
          <p className="text-xs text-ink-500 uppercase tracking-wide">Client</p>
          <p className="text-sm text-ink-300 truncate font-mono">{deal.client_email}</p>
        </div>
      </div>

      <div className="flex items-center justify-between pt-4 border-t border-vault-border">
        <p className="text-xs text-ink-500 font-mono">
          Created {formatRelativeTime(deal.created_at)}
        </p>
        {deal.share_token && (
          <button
            className="text-xs text-gold-500 hover:text-gold-400 font-medium underline underline-offset-1"
            onClick={(e) => {
              e.stopPropagation();
              navigator.clipboard.writeText(deal.share_token);
            }}
          >
            Copy share link
          </button>
        )}
      </div>
    </article>
  );
}

function formatRelativeTime(dateString: string): string {
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return "just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}