export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly data?: unknown
  ) {
    super(message);
    this.name = "ApiError";
  }
}

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
const TOKEN_KEY = "ganji_tokens";
const USER_KEY = "ganji_user";
export const AUTH_TOKENS_UPDATED_EVENT = "ganji:tokens-refreshed";
export const AUTH_SESSION_EXPIRED_EVENT = "ganji:session-expired";

let refreshInFlight: Promise<AuthTokens | null> | null = null;

function expireSession() {
  if (typeof window === "undefined") return;
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
  window.dispatchEvent(new Event(AUTH_SESSION_EXPIRED_EVENT));
}

function refreshSession(): Promise<AuthTokens | null> {
  if (refreshInFlight) return refreshInFlight;

  refreshInFlight = (async () => {
    if (typeof window === "undefined") return null;

    let refreshToken: string | undefined;
    try {
      refreshToken = (JSON.parse(localStorage.getItem(TOKEN_KEY) || "null") as AuthTokens | null)?.refresh_token;
    } catch {
      refreshToken = undefined;
    }

    if (!refreshToken) {
      expireSession();
      return null;
    }

    const response = await fetch(`${API_BASE}/auth/refresh-token`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "include",
      body: JSON.stringify({ refresh_token: refreshToken }),
    });

    if (response.status === 401) {
      expireSession();
      return null;
    }

    if (!response.ok) {
      let message = "Could not refresh your session";
      try {
        const data = await response.json();
        message = data.error || data.message || message;
      } catch {
        message = response.statusText || message;
      }
      throw new ApiError(message, response.status);
    }

    const data = await response.json() as { access_token: string; refresh_token: string };
    const tokens = { access_token: data.access_token, refresh_token: data.refresh_token };
    localStorage.setItem(TOKEN_KEY, JSON.stringify(tokens));
    window.dispatchEvent(new CustomEvent<AuthTokens>(AUTH_TOKENS_UPDATED_EVENT, { detail: tokens }));
    return tokens;
  })().finally(() => {
    refreshInFlight = null;
  });

  return refreshInFlight;
}

async function fetchWithSession(path: string, options: RequestInit = {}): Promise<Response> {
  const isFormData = typeof FormData !== "undefined" && options.body instanceof FormData;
  const headers = new Headers({
    ...(isFormData ? {} : { "Content-Type": "application/json" }),
    ...Object.fromEntries(new Headers(options.headers).entries()),
  });
  const request = (authToken?: string) => fetch(`${API_BASE}${path}`, {
    ...options,
    headers: authToken ? { ...Object.fromEntries(headers.entries()), Authorization: `Bearer ${authToken}` } : headers,
    credentials: "include",
  });

  let res = await request();
  if (res.status === 401 && headers.has("Authorization") && path !== "/auth/refresh-token") {
    const refreshed = await refreshSession();
    if (!refreshed) throw new ApiError("Session expired. Please log in again.", 401);
    res = await request(refreshed.access_token);
    if (res.status === 401) {
      expireSession();
      throw new ApiError("Session expired. Please log in again.", 401);
    }
  }

  return res;
}

async function fetchJson<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetchWithSession(path, options);

  if (!res.ok) {
    let message = "Something went wrong";
    try {
      const data = await res.json();
      message = data.error || data.message || message;
    } catch {
      message = res.statusText || message;
    }
    throw new ApiError(message, res.status);
  }

  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  get<T>(path: string, token?: string) {
    return fetchJson<T>(path, {
      method: "GET",
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    });
  },

  post<T>(path: string, body: unknown, token?: string) {
    const isFormData = typeof FormData !== "undefined" && body instanceof FormData;
    return fetchJson<T>(path, {
      method: "POST",
      body: isFormData ? body : JSON.stringify(body),
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    });
  },

  put<T>(path: string, body: unknown, token?: string) {
    return fetchJson<T>(path, {
      method: "PUT",
      body: JSON.stringify(body),
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    });
  },

  patch<T>(path: string, body: unknown, token?: string) {
    return fetchJson<T>(path, {
      method: "PATCH",
      body: JSON.stringify(body),
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    });
  },

  delete<T>(path: string, token?: string) {
    return fetchJson<T>(path, {
      method: "DELETE",
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    });
  },

  // Deals
  listDeals(token: string) {
    return this.get<ListDealsResponse>("/deals", token);
  },

  createDeal(data: CreateDealRequest, token: string) {
    return this.post<CreateDealResponse>("/deals", data, token);
  },

  getDeal(dealId: string, token: string) {
    return this.get<DealDetailResponse>(`/deals/${dealId}`, token);
  },

  submitWork(dealId: string, token: string) {
    return this.post<SubmitWorkResponse>(`/deals/${dealId}/submit`, {}, token);
  },

  listArtifacts(dealId: string, token: string) {
    return this.get<{ artifacts: Artifact[] }>(`/deals/${dealId}/artifacts`, token);
  },

  uploadArtifact(dealId: string, kind: ArtifactKind, file: File, token: string) {
    const form = new FormData();
    form.append("kind", kind);
    form.append("artifact", file, file.name);
    return this.post<{ artifact: Artifact }>(`/deals/${dealId}/artifacts`, form, token);
  },

  deleteArtifact(dealId: string, artifactId: string, token: string) {
    return this.delete<void>(`/deals/${dealId}/artifacts/${artifactId}`, token);
  },

  async downloadArtifact(dealId: string, artifactId: string, token: string): Promise<Blob> {
    const res = await fetchWithSession(`/deals/${dealId}/artifacts/${artifactId}/download`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      let message = "Failed to download artifact";
      try {
        const data = await res.json();
        message = data.error || data.message || message;
      } catch {
        message = res.statusText || message;
      }
      throw new ApiError(message, res.status);
    }
    return res.blob();
  },

  approveDeal(dealId: string, token: string) {
    return this.post<ApproveDealResponse>(`/deals/${dealId}/approve`, {}, token);
  },

  disputeDeal(dealId: string, reason: string, token: string) {
    return this.post<DisputeDealResponse>(`/deals/${dealId}/dispute`, { reason }, token);
  },

  updatePayeeInvoice(dealId: string, data: UpdatePayeeInvoiceRequest, token: string) {
    return this.patch<UpdatePayeeInvoiceResponse>(`/deals/${dealId}/payee-invoice`, data, token);
  },

  rotateShareLink(dealId: string, token: string) {
    return this.post<RotateShareLinkResponse>(`/deals/${dealId}/share-link`, {}, token);
  },

  getPublicDeal(shareToken: string) {
    return this.get<{ deal: PublicDeal }>(`/public/deals/${shareToken}`);
  },

  getPublicMilestones(shareToken: string) {
    return this.get<{ milestones: MilestoneView[] }>(`/public/deals/${shareToken}/milestones`);
  },

  listPublicArtifacts(shareToken: string) {
    return this.get<{ artifacts: Artifact[] }>(`/public/deals/${shareToken}/artifacts`);
  },

  async downloadPublicArtifact(shareToken: string, artifactId: string): Promise<Blob> {
    const res = await fetchWithSession(`/public/deals/${shareToken}/artifacts/${artifactId}/download`);
    if (!res.ok) {
      let message = "Failed to download artifact";
      try {
        const data = await res.json();
        message = data.error || data.message || message;
      } catch {
        message = res.statusText || message;
      }
      throw new ApiError(message, res.status);
    }
    return res.blob();
  },

  fundPublicDeal(shareToken: string, data: FundDealRequest) {
    return this.post<FundDealResponse>(`/public/deals/${shareToken}/fund`, data);
  },

  generateInvoice(data: GenerateInvoiceRequest, token: string) {
    return this.post<GenerateInvoiceResponse>("/deals/wallet/invoice", data, token);
  },

  publicApproveDeal(shareToken: string) {
    return this.post<PublicApproveDealResponse>(`/public/deals/${shareToken}/approve`, {});
  },

  publicResetPayout(shareToken: string, milestoneId?: string) {
    return this.post<PublicResetPayoutResponse>(`/public/deals/${shareToken}/payout/reset`, {
      milestone_id: milestoneId || "",
    });
  },

  publicAcceptNextMilestone(shareToken: string, milestoneId: string) {
    return this.post<{ deal: PublicDeal }>(`/public/deals/${shareToken}/milestones/${milestoneId}/accept-next`, {});
  },

  publicRejectMilestone(shareToken: string, milestoneId: string, reason: string) {
    return this.post<{ deal: PublicDeal }>(`/public/deals/${shareToken}/milestones/${milestoneId}/reject`, { reason });
  },

  publicExitDeal(shareToken: string) {
    return this.post<{ deal: PublicDeal }>(`/public/deals/${shareToken}/exit`, {});
  },

  // Auth
  getMe(token: string) {
    return this.get<{ user: User }>("/auth/me", token);
  },

  // Live CV (public)
  getCVProfile(slug: string) {
    return this.get<{ profile: CVProfile }>(`/cv/${slug}`);
  },

  verifyEntry(slug: string, entryId: string) {
    return this.get<{ verification: VerifyResult }>(`/cv/${slug}/verify/${entryId}`);
  },

  // Operator (authed, operator-only)
  listDisputes(token: string) {
    return this.get<{ disputes: DealView[] }>("/disputes", token);
  },

  resolveDispute(dealId: string, resolution: "release" | "refund", token: string) {
    return this.post<{ message: string; deal: DealView }>(`/disputes/${dealId}/resolve`, { resolution }, token);
  },

  reconcileDeal(dealId: string, action: "confirm_payout" | "reset_payout", payoutCheckingId: string | undefined, token: string) {
    return this.post<{ deal: DealView }>(`/deals/${dealId}/reconcile`, { action, payout_checking_id: payoutCheckingId }, token);
  },

  // Milestones
  createMilestone(dealId: string, title: string, amountSats: number, order: number, token: string) {
    return this.post<{ milestone: MilestoneView }>(`/deals/${dealId}/milestones`, { title, amount_sats: amountSats, order }, token);
  },

  listMilestones(dealId: string, token: string) {
    return this.get<{ milestones: MilestoneView[] }>(`/deals/${dealId}/milestones`, token);
  },

  updateMilestone(dealId: string, milestoneId: string, status: string, completedAt: string | undefined, token: string) {
    return this.patch<{ milestone: MilestoneView }>(`/deals/${dealId}/milestones/${milestoneId}`, { status, completed_at: completedAt }, token);
  },

  resolveMilestone(dealId: string, milestoneId: string, resolution: "release" | "refund", token: string) {
    return this.post<{ message: string; deal: DealView }>(`/deals/${dealId}/milestones/${milestoneId}/resolve`, { resolution }, token);
  },

  exitDeal(dealId: string, token: string) {
    return this.post<{ message: string }>(`/deals/${dealId}/exit`, {}, token);
  },
};

export type AuthTokens = {
  access_token: string;
  refresh_token: string;
};

export type User = {
  id: string;
  email: string;
  display_name: string;
  slug: string;
  bitcoin_address: string;
  trust_score: number;
  is_operator: boolean;
  created_at: string;
  default_payee_invoice?: string;
};

export type RegisterRequest = {
  email: string;
  password: string;
  display_name: string;
};

export type LoginRequest = {
  email: string;
  password: string;
};

export type DealStatus =
  | "awaiting_payment"
  | "locked"
  | "work_submitted"
  | "reviewing"
  | "released"
  | "disputed"
  | "refunded";

export type DealView = {
  id: string;
  freelancer_id: string;
  client_email: string;
  title: string;
  amount_sats: number;
  source_platform: string;
  preimage_hash: string;
  invoice: string;
  checking_id: string;
  share_token: string;
  status: DealStatus;
  dispute_reason: string;
  disputed_at: string | null;
  resolved_at: string | null;
  resolved_by: string | null;
  created_at: string;
  verified_at: string | null;
  // Milestone mode: per-milestone truth. `invoice`/`checking_id`/`status`
  // mirror the active milestone (first non-terminal) for legacy consumers.
  milestones?: MilestoneView[];
};

export type CreateDealRequest = {
  title: string;
  amount_sats: number;
  source_platform: string;
  client_email: string;
  payee_invoice: string;
  milestones?: { title: string; amount_sats: number }[];
};

export type CreateDealResponse = {
  deal: DealView;
};

export type ListDealsResponse = {
  deals: DealView[];
};

export type DealDetailResponse = {
  deal: DealView;
};

export type SubmitWorkResponse = {
  message: string;
  deal: DealView;
};

export type ArtifactKind = "source_code" | "source_file";

export type Artifact = {
  id: string;
  deal_id: string;
  milestone_id?: string | null;
  kind: ArtifactKind;
  storage_key: string;
  uploaded_at: string;
};

export type ApproveDealResponse = {
  message: string;
  deal: DealView;
};

export type PublicApproveDealResponse = {
  message: string;
  deal: PublicDeal;
};

export type PublicResetPayoutResponse = {
  message: string;
  deal: PublicDeal;
};

export type FundDealRequest = {
  wallet_url: string;
  wallet_key: string;
  milestone_id?: string;
};

export type FundDealResponse = {
  message: string;
  deal: PublicDeal;
};

export type GenerateInvoiceRequest = {
  wallet_url: string;
  wallet_key: string;
  amount_sats: number;
  memo?: string;
};

export type GenerateInvoiceResponse = {
  invoice: {
    bolt11: string;
    payment_hash: string;
    checking_id: string;
  };
};

export type DisputeDealResponse = {
  message: string;
  deal: DealView;
};

export type UpdatePayeeInvoiceRequest = {
  payee_invoice: string;
};

export type UpdatePayeeInvoiceResponse = {
  message: string;
  deal: DealView;
};

export type RotateShareLinkResponse = {
  message: string;
  deal: DealView;
};

export type MilestoneView = {
  id: string;
  title: string;
  amount_sats: number;
  order: number;
  status: string;
  invoice?: string;
  completed_at: string | null;
  created_at: string;
  payout_reset_available?: boolean;
};

export type PublicDeal = {
  title: string;
  amount_sats: number;
  source_platform: string;
  invoice: string;
  status: DealStatus;
  dispute_reason: string;
  created_at: string;
  milestones?: MilestoneView[];
  payout_reset_available?: boolean;
};

export type CVEntry = {
  id: string;
  deal_title: string;
  amount_sats: number;
  source_platform: string;
  artifact_kind: string;
  hash: string;
  algorithm: string;
  verified_at: string;
  created_at: string;
  ots_proof?: string;
  ots_submitted_at?: string | null;
  ots_confirmed_at?: string | null;
};

export type CVProfile = {
  display_name: string;
  slug: string;
  trust_score: number;
  entries: CVEntry[];
};

export type VerifyResult = {
  valid: boolean;
  entry_id: string;
  slug: string;
  hash: string;
  algorithm: string;
  matches_current: boolean;
  deal_title: string;
  verified_at: string;
  ots_verified: boolean;
  ots_proof?: string;
  ots_confirmed_at?: string | null;
  ots_block_height?: number;
};
