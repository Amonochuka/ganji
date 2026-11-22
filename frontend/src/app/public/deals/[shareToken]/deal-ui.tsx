"use client";

import { Button } from "@/components/ui/button";
import { StatusIcon } from "@/components/ui/status-icon";
import type { DealStatus, MilestoneView } from "@/lib/api/api-client";
import { formatCurrency } from "@/lib/utils/format";

export function MilestoneRow({
  ms,
  isLast,
  canAct,
  actionLoading,
  onFund,
  onAcceptNext,
  onReject,
  onResetPayout,
}: {
  ms: MilestoneView;
  isLast: boolean;
  canAct: boolean;
  actionLoading: string | null;
  onFund: () => void;
  onAcceptNext: () => void;
  onReject: () => void;
  onResetPayout: () => void;
}) {
  const needsFunding = ms.status === "awaiting_payment" && !!ms.invoice;
  const actionable =
    canAct &&
    (ms.status === "in_progress" ||
      ms.status === "pending" ||
      ms.status === "completed" ||
      ms.status === "funded" ||
      ms.status === "work_submitted" ||
      ms.status === "awaiting_payment");
  return (
    <div className="p-4 rounded-lg border border-vault-border bg-vault-raised/40">
      <div className="flex justify-between items-start gap-3">
        <div>
          <h3 className="font-medium text-ink-100">
            #{ms.order} {ms.title}
          </h3>
          <p className="text-sm text-ink-500">{formatCurrency(ms.amount_sats)}</p>
        </div>
        <span className={`inline-flex items-center px-2 py-1 rounded text-xs font-medium ${msStatusClass(ms.status)}`}>
          {msStatusLabel(ms.status)}
        </span>
      </div>
      <p className="mt-2 text-xs text-ink-500">
        Delivery estimate: {ms.delivery_days} days from funding
        {ms.due_at ? ` · Due ${new Date(ms.due_at).toLocaleDateString()}` : ""}
      </p>
      {ms.completed_at && <p className="mt-1 text-xs text-ink-500 font-mono">Completed: {new Date(ms.completed_at).toLocaleString()}</p>}
      {needsFunding && (
        <div className="mt-3 p-3 rounded border border-gold-500/30 bg-gold-500/5">
          <p className="text-xs text-ink-400 mb-2">Fund this milestone (sequential hold invoice):</p>
          <div className="flex gap-2">
            <input
              type="text"
              readOnly
              value={ms.invoice || ""}
              className="flex-1 rounded border border-vault-border bg-vault-bg px-2 py-1 text-xs font-mono text-ink-400"
            />
            <Button variant="ghost" size="sm" onClick={() => navigator.clipboard.writeText(ms.invoice || "")}>
              Copy
            </Button>
          </div>
          <Button
            size="sm"
            className="mt-2 w-full"
            onClick={onFund}
            isLoading={actionLoading === `fund-${ms.id}`}
            disabled={!!actionLoading}
          >
            Fund from linked wallet
          </Button>
        </div>
      )}
      {canAct && ms.payout_reset_available && (
        <div className="mt-3 p-3 rounded border border-yellow-600/30 bg-yellow-600/10">
          <p className="text-xs text-ink-300 mb-2">
            A payout attempt was recorded but no payment was confirmed. Clear it before approving again. This does not move funds or change the milestone status.
          </p>
          <Button
            size="sm"
            variant="outline"
            onClick={onResetPayout}
            isLoading={actionLoading === `reset-${ms.id}`}
            disabled={!!actionLoading}
          >
            Clear failed payout attempt
          </Button>
        </div>
      )}
      {actionable && !needsFunding && !ms.payout_reset_available && (
        <div className="mt-3 flex flex-wrap gap-2">
          <Button size="sm" onClick={onAcceptNext} isLoading={actionLoading === `accept-${ms.id}`} disabled={!!actionLoading}>
            {ms.status === "released"
              ? isLast
                ? "Released (paid)"
                : "Released — Start Next"
              : ms.status === "completed"
                ? isLast
                  ? "Finalize payout"
                  : "Release & Start Next"
                : "Approve / Release Milestone"}
          </Button>
          {ms.status !== "exited" && ms.status !== "rejected" && ms.status !== "released" && (
            <Button size="sm" variant="outline" onClick={onReject} isLoading={actionLoading === `reject-${ms.id}`} disabled={!!actionLoading}>
              Reject
            </Button>
          )}
        </div>
      )}
      {isLast && ms.status === "released" && <p className="mt-2 text-xs text-green-400">Final milestone released (paid).</p>}
      {ms.status === "completed" && <p className="mt-2 text-xs text-blue-400">Approved — payout pending (not paid until released).</p>}
      {(ms.status === "disputed" || ms.status === "rejected") && (
        <p className="mt-2 text-xs text-red-400">On hold — awaiting independent review resolution. No auto-refund/release.</p>
      )}
    </div>
  );
}

export function msStatusLabel(s: string) {
  return (
    ({
      pending: "Pending",
      awaiting_payment: "Awaiting payment",
      funded: "Funded",
      in_progress: "In Progress",
      work_submitted: "Submitted",
      completed: "Approved (payout pending)",
      released: "Released (paid)",
      exited: "Exited",
      rejected: "Rejected (under review)",
      disputed: "Under independent review",
      refunded: "Refunded",
    } as Record<string, string>)[s] || s
  );
}

export function msStatusClass(s: string) {
  return (
    ({
      pending: "bg-yellow-600/20 text-yellow-400",
      awaiting_payment: "bg-yellow-600/20 text-yellow-400",
      funded: "bg-blue-600/20 text-blue-400",
      in_progress: "bg-blue-600/20 text-blue-400",
      work_submitted: "bg-gold-500/20 text-gold-400",
      completed: "bg-blue-600/20 text-blue-400",
      released: "bg-green-600/20 text-green-400",
      exited: "bg-red-600/20 text-red-400",
      rejected: "bg-orange-600/20 text-orange-400",
      disputed: "bg-red-600/20 text-red-400",
      refunded: "bg-gray-600/20 text-gray-400",
    } as Record<string, string>)[s] || "bg-gray-600/20 text-gray-400"
  );
}

export function getStatusLabel(status: DealStatus): string {
  return (
    ({
      awaiting_payment: "Awaiting Payment",
      locked: "Funds Held",
      work_submitted: "Work Submitted",
      reviewing: "Under Review",
      released: "Released",
      disputed: "Disputed",
      refunded: "Refunded",
    } as Record<DealStatus, string>)[status]
  );
}

export function getStatusClass(status: DealStatus): string {
  return (
    ({
      awaiting_payment: "bg-yellow-600/20 text-yellow-400 border-yellow-600/30",
      locked: "bg-blue-600/20 text-blue-400 border-blue-600/30",
      work_submitted: "bg-purple-600/20 text-purple-400 border-purple-600/30",
      reviewing: "bg-indigo-600/20 text-indigo-400 border-indigo-600/30",
      released: "bg-green-600/20 text-green-400 border-green-600/30",
      disputed: "bg-red-600/20 text-red-400 border-red-600/30",
      refunded: "bg-gray-600/20 text-gray-400 border-gray-600/30",
    } as Record<DealStatus, string>)[status]
  );
}
