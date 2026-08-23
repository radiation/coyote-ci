import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { PublicBuildDetailPage } from "./PublicBuildDetailPage";
import { AuthContext } from "../auth-context";
import { getPublicBuild, getPublicProject } from "../api";
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL } from "../utils/build";

vi.mock("../api", () => ({
  getPublicBuild: vi.fn(),
  getPublicProject: vi.fn(),
  isAPIErrorStatus: (error: { status?: number }, status: number) =>
    error?.status === status,
}));

function renderPage(
  authStatus: "authenticated" | "unauthenticated" = "unauthenticated",
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  const result = render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={{
          currentUser: null,
          authMode: null,
          authStatus,
          error: null,
          isGlobalAdmin: false,
          loginAvailable: false,
          login: vi.fn(),
          logout: vi.fn(),
          refreshCurrentUser: vi.fn(),
        }}
      >
        <MemoryRouter initialEntries={["/projects/platform/builds/build-1"]}>
          <Routes>
            <Route
              path="/projects/:slug/builds/:buildID"
              element={<PublicBuildDetailPage />}
            />
            <Route
              path="/builds/:id"
              element={<div>Authenticated build page</div>}
            />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );

  return result;
}

describe("PublicBuildDetailPage", () => {
  const mockedGetPublicBuild = vi.mocked(getPublicBuild);
  const mockedGetPublicProject = vi.mocked(getPublicProject);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedGetPublicBuild.mockResolvedValue({
      id: "build-1",
      number: 42,
      status: "success",
      job_name: "release",
      attempt: 2,
      created_at: "2026-05-01T00:00:00Z",
      started_at: "2026-05-01T00:00:02Z",
      completed_at: "2026-05-01T00:01:00Z",
      steps: [
        {
          index: 0,
          name: "compile",
          status: "success",
          started_at: "2026-05-01T00:00:02Z",
          completed_at: "2026-05-01T00:00:30Z",
        },
      ],
    });
    mockedGetPublicProject.mockResolvedValue({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "Core platform pipelines",
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("loads the public build and renders redacted step timing only", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Build #42")).toBeTruthy();
      expect(screen.getByText("compile")).toBeTruthy();
    });
    expect(mockedGetPublicBuild).toHaveBeenCalledWith("platform", "build-1");
    expect(screen.queryByText("Logs")).toBeNull();
    expect(screen.queryByText("Artifacts")).toBeNull();
    expect(screen.queryByRole("button", { name: /Cancel|Rerun/ })).toBeNull();
  });

  it("polls active public builds quickly and terminal builds slowly", async () => {
    vi.useFakeTimers();
    mockedGetPublicBuild
      .mockResolvedValueOnce({
        id: "build-1",
        number: 42,
        status: "running",
        attempt: 2,
        created_at: "2026-05-01T00:00:00Z",
      })
      .mockResolvedValueOnce({
        id: "build-1",
        number: 42,
        status: "success",
        attempt: 2,
        created_at: "2026-05-01T00:00:00Z",
      });

    renderPage();
    expect(mockedGetPublicBuild).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(FAST_POLL_INTERVAL);
    });
    expect(mockedGetPublicBuild).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(FAST_POLL_INTERVAL);
    });
    expect(mockedGetPublicBuild).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(
        SLOW_POLL_INTERVAL - FAST_POLL_INTERVAL,
      );
    });
    expect(mockedGetPublicBuild).toHaveBeenCalledTimes(3);
  });

  it("renders not found for a public 404", async () => {
    mockedGetPublicBuild.mockRejectedValueOnce({ status: 404 });
    renderPage();

    expect(await screen.findByText("Build not found.")).toBeTruthy();
  });

  it("renders public build fallback states", async () => {
    mockedGetPublicBuild.mockRejectedValueOnce(new Error("unavailable"));
    renderPage();
    expect(
      await screen.findByText("Failed to load build: Error: unavailable"),
    ).toBeTruthy();

    mockedGetPublicBuild.mockResolvedValueOnce({
      id: "build-1",
      number: 42,
      status: "success",
      attempt: 1,
      created_at: "2026-05-01T00:00:00Z",
      steps: [],
    });
    renderPage();
    expect(
      await screen.findByText("No step information is available."),
    ).toBeTruthy();
  });

  it("redirects authenticated viewers to the normal build route", async () => {
    renderPage("authenticated");

    expect(await screen.findByText("Authenticated build page")).toBeTruthy();
    expect(mockedGetPublicProject).toHaveBeenCalledWith("platform");
    expect(mockedGetPublicBuild).not.toHaveBeenCalled();
  });

  it("shows authenticated public-build resolver failures", async () => {
    mockedGetPublicProject.mockRejectedValueOnce(new Error("unavailable"));
    renderPage("authenticated");

    expect(
      await screen.findByText("Failed to load build: Error: unavailable"),
    ).toBeTruthy();
    expect(mockedGetPublicBuild).not.toHaveBeenCalled();
  });

  it("renders not found when the authenticated public-project resolver returns 404", async () => {
    mockedGetPublicProject.mockRejectedValueOnce({ status: 404 });
    renderPage("authenticated");

    expect(await screen.findByText("Build not found.")).toBeTruthy();
    expect(mockedGetPublicBuild).not.toHaveBeenCalled();
  });
});
