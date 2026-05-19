import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { ProjectList } from "./ProjectList";

describe("ProjectList", () => {
  it("renders project cards and falls back when description is blank", () => {
    render(
      <MemoryRouter>
        <ProjectList
          projects={[
            {
              id: "project/one",
              name: "Platform",
              slug: "platform",
              description: "   ",
              created_at: "2026-05-01T00:00:00Z",
              updated_at: "2026-05-01T00:00:00Z",
            },
          ]}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("link", { name: "Platform" })).toHaveAttribute(
      "href",
      "/projects/project/one",
    );
    expect(screen.getByText("No project description yet.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "View builds" })).toHaveAttribute(
      "href",
      "/builds?project_id=project%2Fone",
    );
  });
});
