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
const USER_KEY = "ganji_user";
export const AUTH_TOKENS_UPDATED_EVENT = "ganji:tokens-refreshed";
export const AUTH_SESSION_EXPIRED_EVENT = "ganji:session-expired";

let accessToken: string | null = null;
let refreshInFlight: Promise<AuthTokens | null> | null = null;

export function getAccessToken(): string | null {
  return accessToken;
}

export function setAccessToken(token: string | null): void {
  accessToken = token;
}

function expireSession(): void {
  accessToken = null;
  if (typeof window === "undefined") return;
  localStorage.removeItem(USER_KEY);
  window.dispatchEvent(new Event(AUTH_SESSION_EXPIRED_EVENT));
}

export function refreshSession(): Promise<AuthTokens | null> {
  if (refreshInFlight) return refreshInFlight;
  if (typeof window === "undefined") return Promise.resolve(null);

  refreshInFlight = (async () => {
    const response = await fetch(`${API_BASE}/auth/refresh-token`, {
      method: "POST",
      credentials: "include",
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

    const data = await response.json() as { access_token: string; refresh_token?: string };
    const tokens: AuthTokens = { access_token: data.access_token };
    accessToken = data.access_token;
    window.dispatchEvent(new CustomEvent<AuthTokens>(AUTH_TOKENS_UPDATED_EVENT, { detail: tokens }));
    return tokens;
  })().finally(() => {
    refreshInFlight = null;
  });

  return refreshInFlight;
}

export async function restoreSession(): Promise<AuthTokens | null> {
  try {
    return await refreshSession();
  } catch {
    expireSession();
    return null;
  }
}

export async function logoutSession(): Promise<void> {
  const token = accessToken;
  if (typeof window !== "undefined") {
    try {
      await fetch(`${API_BASE}/auth/logout`, {
        method: "POST",
        headers: token ? { Authorization: `Bearer ${token}` } : undefined,
        credentials: "include",
      });
    } catch {
    }
    localStorage.removeItem(USER_KEY);
    window.dispatchEvent(new Event(AUTH_SESSION_EXPIRED_EVENT));
  }
  accessToken = null;
}

export async function fetchWithSession(path: string, options: RequestInit = {}): Promise<Response> {
  const isFormData = typeof FormData !== "undefined" && options.body instanceof FormData;
  const headers = new Headers({
    ...(isFormData ? {} : { "Content-Type": "application/json" }),
    ...Object.fromEntries(new Headers(options.headers).entries()),
  });
  if (!headers.has("Authorization") && accessToken) {
    headers.set("Authorization", `Bearer ${accessToken}`);
  }

  const request = (token?: string) => {
    const requestHeaders = new Headers(headers);
    if (token) requestHeaders.set("Authorization", `Bearer ${token}`);
    return fetch(`${API_BASE}${path}`, {
      ...options,
      headers: requestHeaders,
      credentials: "include",
    });
  };

  const usedToken = headers.get("Authorization") ? undefined : accessToken || undefined;
  let res = await request(usedToken);
  if (res.status === 401 && headers.has("Authorization") && path !== "/auth/refresh-token" && path !== "/auth/login" && path !== "/auth/signup") {
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
