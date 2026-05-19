import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { BuildActivityList } from "./BuildActivityList";

describe("BuildActivityList", () => {
  it("renders the empty state when no activity exists", () => {
    render(
      <MemoryRouter>
        <BuildActivityList
          title="Queue activity"
          items={[]}
          emptyMessage="Nothing running"
        />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("heading", { name: "Queue activity" }),
    ).toBeTruthy();
    expect(screen.getByText("Nothing running")).toBeTruthy();
  });

  it("renders queue entries using slug and created-at fallbacks", () => {
    render(
      <MemoryRouter>
        <BuildActivityList
          title="Queue activity"
          emptyMessage="Nothing running"
          items={[
            {
              kind: "queue",
              entry: {
                build_id: "build-1",
                build_number: 14,
                project_id: "project-1",
                project_slug: "platform",
                priority: 1,
                status: "queued",
                created_at: "2026-05-01T00:00:00Z",
              },
            },
          ]}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("link", { name: "Build #14" })).toHaveAttribute(
      "href",
      "/builds/build-1",
    );
    expect(screen.getByRole("link", { name: "platform" })).toHaveAttribute(
      "href",
      "/projects/project-1",
    );
    expect(screen.queryByText(/·/)).toBeNull();
  });

  it("renders queue entries with the job name when available", () => {
    const { container } = render(
      <MemoryRouter>
        <BuildActivityList
          title="Queue activity"
          emptyMessage="Nothing running"
          items={[
            {
              kind: "queue",
              entry: {
                build_id: "build-2",
                build_number: 15,
                project_id: "project-2",
                project_name: "Platform",
                job_name: "release",
                priority: 1,
                status: "running",
                created_at: "2026-05-01T00:00:00Z",
                queued_at: "2026-05-01T00:00:10Z",
              },
            },
          ]}
        />
      </MemoryRouter>,
    );

    expect(
      container.querySelector(".activity-list-main .subtle-text")?.textContent,
    ).toContain("Platform · release");
  });

  it("renders builds using id and project-id fallbacks plus trigger ref", () => {
    render(
      <MemoryRouter>
        <BuildActivityList
          title="Recent builds"
          emptyMessage="No builds"
          items={[
            {
              kind: "build",
              build: {
                id: "1234567890abcdef",
                project_id: "project-9",
                priority: 1,
                status: "failed",
                created_at: "2026-05-01T00:00:00Z",
                queued_at: null,
                started_at: null,
                finished_at: null,
                current_step_index: 0,
                error_message: null,
                trigger_ref: "release/1.0",
              },
            },
          ]}
        />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("link", { name: "Build 12345678" }),
    ).toHaveAttribute("href", "/builds/1234567890abcdef");
    expect(screen.getByRole("link", { name: "project-9" })).toHaveAttribute(
      "href",
      "/projects/project-9",
    );
    expect(screen.getByText(/release\/1.0/)).toBeTruthy();
  });
});
