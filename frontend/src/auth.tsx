import { useCallback, useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { APIError, getAuthConfig, getMe, logoutSession } from "./api";
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
    data: authConfig,
    error: authConfigError,
    isError: isAuthConfigError,
    isPending: isAuthConfigPending,
    refetch: refetchAuthConfig,
  } = useQuery({
    queryKey: ["auth-config"],
    queryFn: getAuthConfig,
    retry: false,
  });

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

  const { authMode, authStatus, currentUser, error, loginUrl } = useMemo(() => {
    if (isAuthConfigPending) {
      return {
        authMode: null,
        authStatus: "loading" as const,
        currentUser: null,
        error: null,
        loginUrl: null,
      };
    }

    if (isAuthConfigError) {
      return {
        authMode: null,
        authStatus: "error" as const,
        currentUser: null,
        error:
          authConfigError instanceof Error
            ? authConfigError
            : new Error(String(authConfigError)),
        loginUrl: null,
      };
    }

    const nextAuthMode = authConfig?.auth_mode ?? null;
    const nextLoginURL = authConfig?.login_url ?? null;
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
        nextAuthStatus === "authenticated"
          ? (data?.auth_mode ?? nextAuthMode)
          : nextAuthMode,
      authStatus: nextAuthStatus,
      currentUser:
        nextAuthStatus === "authenticated" ? (data?.user ?? null) : null,
      error: nextError,
      loginUrl: nextLoginURL,
    };
  }, [
    authConfig,
    authConfigError,
    data,
    isAuthConfigError,
    isAuthConfigPending,
    isError,
    isPending,
    queryError,
  ]);

  const refreshCurrentUser = useCallback(async () => {
    await Promise.all([refetchAuthConfig(), refetch()]);
  }, [refetch, refetchAuthConfig]);

  const login = useCallback(() => {
    if (loginUrl) {
      navigate(loginUrl);
    }
  }, [loginUrl, navigate]);

  const logout = useCallback(async () => {
    await logoutSession();
    await Promise.all([refetchAuthConfig(), refetch()]);
  }, [refetch, refetchAuthConfig]);

  const value = useMemo<AuthContextValue>(
    () => ({
      currentUser,
      authMode,
      authStatus,
      error,
      isGlobalAdmin: currentUser?.global_role === "admin",
      loginAvailable: loginUrl !== null,
      login,
      logout,
      refreshCurrentUser,
    }),
    [
      authMode,
      authStatus,
      currentUser,
      error,
      loginUrl,
      login,
      logout,
      refreshCurrentUser,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
