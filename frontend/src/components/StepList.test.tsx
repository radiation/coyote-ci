import { expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { StepDetails, StepList } from "./StepList";
import type { BuildStep } from "../types";

const step: BuildStep = {
  id: "one",
  build_id: "build",
  step_index: 0,
  name: "Compile",
  command: "echo ok",
  status: "failed",
  worker_id: "builder",
  started_at: "2026-04-01T00:00:00Z",
  finished_at: "2026-04-01T00:01:05Z",
  exit_code: 1,
  stdout: null,
  stderr: null,
  error_message: "compile failed",
};

it("keeps consecutive groups in order, marks failures and exposes native selection", () => {
  const select = vi.fn();
  const steps = [
    step,
    { ...step, id: "two", step_index: 1, group_name: "Verify" },
    { ...step, id: "three", step_index: 2, group_name: "Verify" },
  ];
  render(<StepList steps={steps} selected={step} onSelect={select} />);
  const buttons = screen.getAllByRole("button");
  expect(buttons[0]).toHaveAttribute("aria-current", "step");
  expect(within(buttons[0]).getByText("1m 5s")).toBeVisible();
  expect(screen.getAllByText("Failed")).toHaveLength(3);
  expect(screen.getByText(/Parallel · 2/)).toBeVisible();
  fireEvent.click(buttons[2]);
  expect(select).toHaveBeenCalledWith(2);
});

it("shows full command, phase timings and failure details", () => {
  render(
    <StepDetails
      step={{
        ...step,
        job: {
          timing: {
            phases: [
              {
                name: "workspace_prepare",
                started_at: step.started_at,
                finished_at: step.finished_at,
              },
            ],
          },
        },
      }}
    />,
  );
  for (const text of [
    "echo ok",
    "compile failed",
    "builder",
    "workspace prepare",
  ])
    expect(screen.getByText(text)).toBeVisible();
  expect(screen.getAllByText("1m 5s")).toHaveLength(2);
});
