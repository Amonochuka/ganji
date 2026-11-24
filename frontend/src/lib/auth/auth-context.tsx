"use client";

import { createContext, useContext, useEffect, useState, ReactNode } from "react";
import {
  api,
  AUTH_SESSION_EXPIRED_EVENT,
  AUTH_TOKENS_UPDATED_EVENT,
  type AuthTokens,
  type User,
  type LoginRequest,
  type RegisterRequest,
} from "@/lib/api/api-client";
import { logoutSession, restoreSession, setAccessToken } from "@/lib/api/session";

interface AuthContextType {
  user: User | null;
  tokens: AuthTokens | null;
  isLoading: boolean;
  login: (credentials: LoginRequest) => Promise<void>;
  signup: (data: RegisterRequest) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

const USER_KEY = "ganji_user";

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [tokens, setTokens] = useState<AuthTokens | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const storedUser = localStorage.getItem(USER_KEY);
    if (storedUser) {
      try {
        setUser(JSON.parse(storedUser));
      } catch {
        localStorage.removeItem(USER_KEY);
      }
    }

    void restoreSession().finally(() => setIsLoading(false));
  }, []);

  useEffect(() => {
    function handleTokensUpdated(event: Event) {
      setTokens((event as CustomEvent<AuthTokens>).detail);
    }

    function handleSessionExpired() {
      setTokens(null);
      setUser(null);
      localStorage.removeItem(USER_KEY);
    }

    window.addEventListener(AUTH_TOKENS_UPDATED_EVENT, handleTokensUpdated);
    window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, handleSessionExpired);
    return () => {
      window.removeEventListener(AUTH_TOKENS_UPDATED_EVENT, handleTokensUpdated);
      window.removeEventListener(AUTH_SESSION_EXPIRED_EVENT, handleSessionExpired);
    };
  }, []);

  async function login(credentials: LoginRequest) {
    const res = await api.post<{ user: User; access_token: string; refresh_token?: string }>("/auth/login", credentials);
    const newTokens: AuthTokens = { access_token: res.access_token };
    setAccessToken(res.access_token);
    setTokens(newTokens);
    setUser(res.user);
    localStorage.setItem(USER_KEY, JSON.stringify(res.user));
  }

  async function signup(data: RegisterRequest) {
    const res = await api.post<{ user: User; access_token: string; refresh_token?: string }>("/auth/signup", data);
    const newTokens: AuthTokens = { access_token: res.access_token };
    setAccessToken(res.access_token);
    setTokens(newTokens);
    setUser(res.user);
    localStorage.setItem(USER_KEY, JSON.stringify(res.user));
  }

  async function logout() {
    await logoutSession();
    setAccessToken(null);
    setTokens(null);
    setUser(null);
  }

  return (
    <AuthContext.Provider value={{ user, tokens, isLoading, login, signup, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
