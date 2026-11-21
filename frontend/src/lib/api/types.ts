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
  payout_reset_available?: boolean;
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
