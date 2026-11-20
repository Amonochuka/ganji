import type { AuthTokens } from "./types";

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

export async function fetchWithSession(path: string, options: RequestInit = {}): Promise<Response> {
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

export async function fetchJson<T>(path: string, options: RequestInit = {}): Promise<T> {
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
