"use client";

interface FormBannerProps {
  message: string;
  type?: "error" | "success" | "warning";
}

export function FormBanner({ message, type = "error" }: FormBannerProps) {
  const styles = {
    error: "bg-status-disputed/10 border-status-disputed/30 text-status-disputed",
    success: "bg-status-released/10 border-status-released/30 text-status-released",
    warning: "bg-status-locked/10 border-status-locked/30 text-status-locked",
  };

  const icons = {
    error: (
      <svg className="h-4 w-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <circle cx="12" cy="12" r="10" />
        <line x1="15" y1="9" x2="9" y2="15" />
        <line x1="9" y1="9" x2="15" y2="15" />
      </svg>
    ),
    success: (
      <svg className="h-4 w-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
        <polyline points="22 4 12 14.01 9 11.01" />
      </svg>
    ),
    warning: (
      <svg className="h-4 w-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
        <line x1="12" y1="9" x2="12" y2="13" />
        <line x1="12" y1="17" x2="12.01" y2="17" />
      </svg>
    ),
  };

  return (
    <div className={`flex items-start gap-3 rounded-lg border p-3 text-sm ${styles[type]}`} role="alert">
      {icons[type]}
      <p>{message}</p>
    </div>
  );
}