"use client";

import { createContext, useContext, useEffect, useState, ReactNode } from "react";
import { api, type AuthTokens, type User, type LoginRequest, type RegisterRequest } from "@/lib/api/api-client";

interface AuthContextType {
  user: User | null;
  tokens: AuthTokens | null;
  isLoading: boolean;
  login: (credentials: LoginRequest) => Promise<void>;
  signup: (data: RegisterRequest) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

const TOKEN_KEY = "ganji_tokens";
const USER_KEY = "ganji_user";

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [tokens, setTokens] = useState<AuthTokens | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const storedTokens = localStorage.getItem(TOKEN_KEY);
    const storedUser = localStorage.getItem(USER_KEY);
    if (storedTokens && storedUser) {
      setTokens(JSON.parse(storedTokens));
      setUser(JSON.parse(storedUser));
    }
    setIsLoading(false);
  }, []);

  async function login(credentials: LoginRequest) {
    const res = await api.post<{ user: User; access_token: string; refresh_token: string }>("/auth/login", credentials);
    const newTokens = { access_token: res.access_token, refresh_token: res.refresh_token };
    setTokens(newTokens);
    setUser(res.user);
    localStorage.setItem(TOKEN_KEY, JSON.stringify(newTokens));
    localStorage.setItem(USER_KEY, JSON.stringify(res.user));
  }

  async function signup(data: RegisterRequest) {
    const res = await api.post<{ user: User; access_token: string; refresh_token: string }>("/auth/signup", data);
    const newTokens = { access_token: res.access_token, refresh_token: res.refresh_token };
    setTokens(newTokens);
    setUser(res.user);
    localStorage.setItem(TOKEN_KEY, JSON.stringify(newTokens));
    localStorage.setItem(USER_KEY, JSON.stringify(res.user));
  }

  function logout() {
    setTokens(null);
    setUser(null);
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
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