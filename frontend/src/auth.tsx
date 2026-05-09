import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { APIError, authLoginURL, getMe, logoutSession } from "./api";
import {
  AuthContext,
  type AuthContextValue,
  type AuthStatus,
} from "./auth-context";
import type { MeResponse, User } from "./types/identity";

export function AuthProvider({
  children,
  navigate = (url: string) => window.location.assign(url),
}: {
  children: ReactNode;
  navigate?: (url: string) => void;
}) {
  const [currentUser, setCurrentUser] = useState<User | null>(null);
  const [authMode, setAuthMode] = useState<MeResponse["auth_mode"] | null>(
    null,
  );
  const [authStatus, setAuthStatus] = useState<AuthStatus>("loading");
  const [error, setError] = useState<Error | null>(null);

  const refreshCurrentUser = useCallback(async () => {
    setAuthStatus("loading");
    setError(null);
    try {
      const me = await getMe();
      setCurrentUser(me.user);
      setAuthMode(me.auth_mode);
      setAuthStatus("authenticated");
    } catch (loadError) {
      setCurrentUser(null);
      if (loadError instanceof APIError && loadError.status === 401) {
        setAuthStatus("unauthenticated");
        return;
      }
      setError(
        loadError instanceof Error ? loadError : new Error(String(loadError)),
      );
      setAuthStatus("error");
    }
  }, []);

  useEffect(() => {
    void refreshCurrentUser();
  }, [refreshCurrentUser]);

  const login = useCallback(() => {
    navigate(authLoginURL());
  }, [navigate]);

  const logout = useCallback(async () => {
    setError(null);
    await logoutSession();
    setCurrentUser(null);
    setAuthStatus("unauthenticated");
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      currentUser,
      authMode,
      authStatus,
      error,
      isGlobalAdmin: currentUser?.global_role === "admin",
      login,
      logout,
      refreshCurrentUser,
    }),
    [
      authMode,
      authStatus,
      currentUser,
      error,
      login,
      logout,
      refreshCurrentUser,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
