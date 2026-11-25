import Link from "next/link";
import type { DealView } from "@/lib/api/api-client";

export function DealHeader({ user, role }: { user: { email?: string } | null; role?: "client" | "freelancer" | "operator" }) {
  const dashboardHref = role === "client" ? "/dashboard?role=client" : role === "freelancer" ? "/dashboard?role=freelancer" : "/dashboard";
  return (
    <header className="border-b border-vault-border bg-vault-bg/80 backdrop-blur-sm sticky top-0 z-50">
      <div className="mx-auto flex h-16 max-w-5xl items-center justify-between px-6">
        <Link href={dashboardHref} className="brand-wordmark" aria-label="Ganji Dashboard">ganji</Link>
        <div className="flex items-center gap-3">
          {role && (
            <span className="text-xs px-2 py-1 rounded-full border border-vault-border text-ink-300">
              {role === "operator" ? "Operator view" : role === "client" ? "Client view" : "Freelancer view"}
            </span>
          )}
          <span className="text-xs font-mono text-ink-500">{user?.email ?? ""}</span>
        </div>
      </div>
    </header>
  );
}

export function getStatusClass(status: string): string {
  const map: Record<string, string> = {
    awaiting_payment: "bg-yellow-600/10 text-yellow-400 border-yellow-600/30",
    locked: "bg-blue-600/10 text-blue-400 border-blue-600/30",
    work_submitted: "bg-gold-500/10 text-gold-400 border-gold-500/30",
    reviewing: "bg-gold-500/10 text-gold-400 border-gold-500/30",
    released: "bg-green-600/10 text-green-400 border-green-600/30",
    disputed: "bg-red-600/10 text-red-400 border-red-600/30",
    refunded: "bg-gray-600/10 text-gray-400 border-gray-600/30",
  };
  return map[status] || "bg-gray-600/10 text-gray-400 border-gray-600/30";
}

export function getStatusLabel(status: string): string {
  const map: Record<string, string> = { awaiting_payment: "Awaiting payment", locked: "Funds held", work_submitted: "Work submitted", reviewing: "Reviewing", released: "Released", disputed: "Disputed", refunded: "Refunded" };
  return map[status] || status.replace(/_/g, " ");
}

export function StatusTimeline({ deal }: { deal: DealView }) {
  const steps = ["awaiting_payment", "locked", "work_submitted", "released"];
  return (
    <ol className="space-y-2 text-sm">
      {steps.map((s) => (
        <li key={s} className={deal.status === s ? "text-gold-400 font-medium" : "text-ink-400"}>{getStatusLabel(s)}</li>
      ))}
      {deal.status === "disputed" && <li className="text-red-400 font-medium">Disputed</li>}
      {deal.status === "refunded" && <li className="text-gray-400 font-medium">Refunded</li>}
    </ol>
  );
}

export function getMilestoneStatusLabel(status: string): string {
  const labels: Record<string, string> = {
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
  };
  return labels[status] || status;
}

export function getMilestoneStatusClass(status: string): string {
  const classes: Record<string, string> = {
    pending: "bg-yellow-600/20 text-yellow-400 border-yellow-600/30",
    awaiting_payment: "bg-yellow-600/20 text-yellow-400 border-yellow-600/30",
    funded: "bg-blue-600/20 text-blue-400 border-blue-600/30",
    in_progress: "bg-blue-600/20 text-blue-400 border-blue-600/30",
    work_submitted: "bg-gold-500/20 text-gold-400 border-gold-500/30",
    completed: "bg-blue-600/20 text-blue-400 border-blue-600/30",
    released: "bg-green-600/20 text-green-400 border-green-600/30",
    exited: "bg-red-600/20 text-red-400 border-red-600/30",
    rejected: "bg-orange-600/20 text-orange-400 border-orange-600/30",
    disputed: "bg-red-600/20 text-red-400 border-red-600/30",
    refunded: "bg-gray-600/20 text-gray-400 border-gray-600/30",
  };
  return classes[status] || "bg-gray-600/20 text-gray-400 border-gray-600/30";
}
