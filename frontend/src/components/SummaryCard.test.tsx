import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { SummaryCard } from "./SummaryCard";

describe("SummaryCard", () => {
  it("uses the default tone and hides optional sections when omitted", () => {
    const { container } = render(<SummaryCard title="Builds" value="12" />);

    expect(screen.getByText("Builds")).toBeTruthy();
    expect(screen.getByText("12")).toBeTruthy();
    expect(container.querySelector(".summary-card-default")).toBeTruthy();
    expect(container.querySelector(".summary-card-description")).toBeNull();
    expect(container.querySelector(".summary-card-footer")).toBeNull();
  });

  it("renders custom tone, description, and footer", () => {
    const { container } = render(
      <SummaryCard
        title="Failures"
        value="2"
        tone="danger"
        description="Needs attention"
        footer={<span>Open builds</span>}
      />,
    );

    expect(container.querySelector(".summary-card-danger")).toBeTruthy();
    expect(screen.getByText("Needs attention")).toBeTruthy();
    expect(screen.getByText("Open builds")).toBeTruthy();
  });
});
