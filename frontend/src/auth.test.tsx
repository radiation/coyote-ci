import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "./auth";
import { useAuth } from "./auth-context";
import { APIError, authLoginURL, getMe, logoutSession } from "./api";

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    authLoginURL: vi.fn(() => "/auth/login"),
    getMe: vi.fn(),
    logoutSession: vi.fn(),
  };
});

function AuthConsumer() {
  const { authStatus, currentUser, isGlobalAdmin, login, logout } = useAuth();
  return (
    <div>
      <p>Status: {authStatus}</p>
      <p>User: {currentUser?.email ?? "none"}</p>
      <p>Admin: {isGlobalAdmin ? "yes" : "no"}</p>
      <button type="button" onClick={login}>
        Login
      </button>
      <button type="button" onClick={() => void logout()}>
        Logout
      </button>
    </div>
  );
}

function renderWithAuthProvider(
  ui: React.ReactNode,
  navigate?: (url: string) => void,
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider navigate={navigate}>{ui}</AuthProvider>
    </QueryClientProvider>,
  );
}

describe("AuthProvider", () => {
  const mockedGetMe = vi.mocked(getMe);
  const mockedLogoutSession = vi.mocked(logoutSession);
  const mockedAuthLoginURL = vi.mocked(authLoginURL);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedGetMe.mockResolvedValue({
      auth_mode: "oidc",
      user: {
        id: "user-1",
        email: "admin@example.com",
        global_role: "admin",
      },
    });
    mockedLogoutSession.mockResolvedValue();
  });

  it("loads the current user", async () => {
    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Status: authenticated")).toBeTruthy();
      expect(screen.getByText("User: admin@example.com")).toBeTruthy();
      expect(screen.getByText("Admin: yes")).toBeTruthy();
    });
  });

  it("sets unauthenticated state when /api/me returns 401", async () => {
    mockedGetMe.mockRejectedValue(new APIError(401, "authentication required"));

    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Status: unauthenticated")).toBeTruthy();
      expect(screen.getByText("User: none")).toBeTruthy();
    });
  });

  it("navigates to login", async () => {
    const navigate = vi.fn();
    renderWithAuthProvider(<AuthConsumer />, navigate);

    await waitFor(() => {
      expect(screen.getByText("Status: authenticated")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Login" }));

    expect(mockedAuthLoginURL).toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith("/auth/login");
  });

  it("logs out and clears auth state", async () => {
    mockedGetMe
      .mockResolvedValueOnce({
        auth_mode: "oidc",
        user: {
          id: "user-1",
          email: "admin@example.com",
          global_role: "admin",
        },
      })
      .mockRejectedValueOnce(new APIError(401, "authentication required"));

    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Status: authenticated")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Logout" }));

    await waitFor(() => {
      expect(mockedLogoutSession).toHaveBeenCalled();
      expect(screen.getByText("Status: unauthenticated")).toBeTruthy();
      expect(screen.getByText("User: none")).toBeTruthy();
    });
  });
});
