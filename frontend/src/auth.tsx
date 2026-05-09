import { useCallback, useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { APIError, authLoginURL, getMe, logoutSession } from "./api";
import {
  AuthContext,
  type AuthContextValue,
  type AuthStatus,
} from "./auth-context";

export function AuthProvider({
  children,
  navigate = (url: string) => window.location.assign(url),
}: {
  children: ReactNode;
  navigate?: (url: string) => void;
}) {
  const {
    data,
    error: queryError,
    isError,
    isPending,
    refetch,
  } = useQuery({
    queryKey: ["me", "auth-provider"],
    queryFn: getMe,
    retry: false,
  });

  const { authMode, authStatus, currentUser, error } = useMemo(() => {
    const nextError =
      queryError instanceof APIError && queryError.status === 401
        ? null
        : queryError instanceof Error
          ? queryError
          : queryError
            ? new Error(String(queryError))
            : null;
    const nextAuthStatus: AuthStatus = isPending
      ? "loading"
      : isError
        ? queryError instanceof APIError && queryError.status === 401
          ? "unauthenticated"
          : "error"
        : "authenticated";

    return {
      authMode:
        nextAuthStatus === "authenticated" ? (data?.auth_mode ?? null) : null,
      authStatus: nextAuthStatus,
      currentUser:
        nextAuthStatus === "authenticated" ? (data?.user ?? null) : null,
      error: nextError,
    };
  }, [data, isError, isPending, queryError]);

  const refreshCurrentUser = useCallback(async () => {
    await refetch();
  }, [refetch]);

  const login = useCallback(() => {
    navigate(authLoginURL());
  }, [navigate]);

  const logout = useCallback(async () => {
    await logoutSession();
    await refetch();
  }, [refetch]);

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
