import { fetchJson, fetchWithSession, ApiError } from "./session";
import type {
  ApproveDealResponse,
  Artifact,
  ArtifactKind,
  CreateDealRequest,
  CreateDealResponse,
  CVProfile,
  DealDetailResponse,
  DealView,
  DisputeDealResponse,
  FundDealRequest,
  FundDealResponse,
  GenerateInvoiceRequest,
  GenerateInvoiceResponse,
  ListDealsResponse,
  MilestoneView,
  PublicApproveDealResponse,
  PublicDeal,
  PublicResetPayoutResponse,
  RotateShareLinkResponse,
  SubmitWorkResponse,
  UpdatePayeeInvoiceRequest,
  UpdatePayeeInvoiceResponse,
  User,
  VerifyResult,
} from "./types";

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

  checkPayment(dealId: string, token: string) {
    return this.get<DealDetailResponse>(`/deals/${dealId}/payment`, token);
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
  createMilestone(dealId: string, title: string, amountSats: number, order: number, token: string, deliveryDays = 7) {
    return this.post<{ milestone: MilestoneView }>(`/deals/${dealId}/milestones`, { title, amount_sats: amountSats, order, delivery_days: deliveryDays }, token);
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
