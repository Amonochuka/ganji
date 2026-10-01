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

async function fetchJson<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options.headers,
    },
    credentials: "include",
  });

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
    return fetchJson<T>(path, {
      method: "POST",
      body: JSON.stringify(body),
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
  created_at: string;
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
};

export type CreateDealRequest = {
  title: string;
  amount_sats: number;
  source_platform: string;
  client_email: string;
  payee_invoice: string;
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

export type ApproveDealResponse = {
  message: string;
  deal: DealView;
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

export type PublicDeal = {
  title: string;
  amount_sats: number;
  source_platform: string;
  invoice: string;
  status: DealStatus;
  dispute_reason: string;
  created_at: string;
};