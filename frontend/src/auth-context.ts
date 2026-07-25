import { createContext, useContext } from "react";
import type { AuthConfigResponse, MeResponse, User } from "./types/identity";

export type AuthStatus =
  "loading" | "authenticated" | "unauthenticated" | "error";

export interface AuthContextValue {
  currentUser: User | null;
  authMode: AuthConfigResponse["auth_mode"] | MeResponse["auth_mode"] | null;
  authStatus: AuthStatus;
  error: Error | null;
  isGlobalAdmin: boolean;
  loginAvailable: boolean;
  login: () => void;
  logout: () => Promise<void>;
  refreshCurrentUser: () => Promise<void>;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
