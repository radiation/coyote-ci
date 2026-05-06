import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { UsersPage } from "./UsersPage";
import { createUser, deleteUser, listUsers, updateUser } from "../api";

vi.mock("../api", () => ({
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  listUsers: vi.fn(),
  updateUser: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <UsersPage />
    </QueryClientProvider>,
  );
}

describe("UsersPage", () => {
  const mockedCreateUser = vi.mocked(createUser);
  const mockedDeleteUser = vi.mocked(deleteUser);
  const mockedListUsers = vi.mocked(listUsers);
  const mockedUpdateUser = vi.mocked(updateUser);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedListUsers.mockResolvedValue([
      {
        id: "user-1",
        email: "admin@example.com",
        display_name: "Admin",
        global_role: "admin",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:00:00Z",
      },
    ]);
    mockedCreateUser.mockResolvedValue({
      id: "user-2",
      email: "dev@example.com",
      global_role: "user",
    });
    mockedUpdateUser.mockResolvedValue({
      id: "user-1",
      email: "admin@example.com",
      global_role: "user",
    });
    mockedDeleteUser.mockResolvedValue();
  });

  it("renders users", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Users")).toBeTruthy();
      expect(screen.getByDisplayValue("admin@example.com")).toBeTruthy();
      expect(screen.getByDisplayValue("Admin")).toBeTruthy();
    });
  });

  it("creates, edits, and deletes users", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByDisplayValue("admin@example.com")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "dev@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Display Name"), {
      target: { value: "Dev" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create User" }));

    await waitFor(() => {
      expect(mockedCreateUser).toHaveBeenCalledWith({
        email: "dev@example.com",
        display_name: "Dev",
        global_role: "user",
      });
    });

    fireEvent.change(
      screen.getByLabelText("Global role for admin@example.com"),
      {
        target: { value: "user" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(mockedUpdateUser).toHaveBeenCalledWith("user-1", {
        email: "admin@example.com",
        display_name: "Admin",
        global_role: "user",
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => {
      expect(mockedDeleteUser).toHaveBeenCalledWith("user-1");
    });
  });
});
