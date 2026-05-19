import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { PageHeader } from "./PageHeader";

describe("PageHeader", () => {
  it("renders all optional sections when provided", () => {
    render(
      <MemoryRouter>
        <PageHeader
          eyebrow="Dashboard"
          title="Overview"
          description="Current activity"
          actions={<a href="/projects">Projects</a>}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("Dashboard")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Overview" })).toBeTruthy();
    expect(screen.getByText("Current activity")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Projects" })).toBeTruthy();
  });

  it("omits optional sections when not provided", () => {
    const { container } = render(<PageHeader title="Builds" />);

    expect(screen.getByRole("heading", { name: "Builds" })).toBeTruthy();
    expect(container.querySelector(".page-eyebrow")).toBeNull();
    expect(container.querySelector(".page-header-actions")).toBeNull();
    expect(container.querySelector(".subtle-text")).toBeNull();
  });
});
