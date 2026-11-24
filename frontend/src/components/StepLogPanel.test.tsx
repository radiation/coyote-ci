import { act, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { buildStepLogStreamURL, getStepLogs } from "../api";
import { StepLogPanel } from "./StepLogPanel";

vi.mock("../api", () => ({
  getStepLogs: vi.fn(),
  buildStepLogStreamURL: vi.fn(),
}));
const history = {
  build_id: "one",
  step_index: 0,
  after: 0,
  chunks: [],
  next_sequence: 4,
};
class FakeStream extends EventTarget {
  static instances: FakeStream[] = [];
  constructor() {
    super();
    FakeStream.instances.push(this);
  }
  close = vi.fn();
  onerror = () => {};
  chunk(sequence_no: number, stream: string, chunk_text: string) {
    this.dispatchEvent(
      new MessageEvent("chunk", {
        data: JSON.stringify({ sequence_no, stream, chunk_text }),
      }),
    );
  }
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  vi.resetAllMocks();
  FakeStream.instances = [];
});

it("streams after history, suppresses duplicates, resumes after disconnect and cleans up", async () => {
  vi.stubGlobal("EventSource", FakeStream);
  vi.mocked(getStepLogs).mockResolvedValue(history);
  const view = render(<StepLogPanel buildID="one" stepIndex={0} />);
  await screen.findByText("No logs yet.");
  expect(getStepLogs).toHaveBeenCalledWith("one", 0, 0, 500);
  expect(buildStepLogStreamURL).toHaveBeenCalledWith("one", 0, 4);
  const [first] = FakeStream.instances;
  act(() => {
    first.chunk(5, "stdout", "started");
    first.chunk(5, "stderr", "duplicate");
    first.chunk(6, "system", "done");
  });
  expect(screen.getByText(/\[stdout\] started/).textContent).toBe(
    "[stdout] started\n[system] done",
  );
  vi.useFakeTimers();
  first.onerror();
  expect(first.close).toHaveBeenCalledOnce();
  vi.advanceTimersByTime(1500);
  expect(buildStepLogStreamURL).toHaveBeenLastCalledWith("one", 0, 6);
  const second = FakeStream.instances[1];
  view.rerender(<StepLogPanel buildID="two" stepIndex={0} />);
  expect(screen.queryByText(/\[stdout\] started/)).toBeNull();
  expect(second.close).toHaveBeenCalledOnce();
  await act(async () => {});
  FakeStream.instances[2].onerror();
  view.unmount();
  vi.advanceTimersByTime(1500);
  expect(FakeStream.instances).toHaveLength(3);
});

it("ignores late history after selection changes", async () => {
  vi.stubGlobal("EventSource", FakeStream);
  let finish!: () => void;
  vi.mocked(getStepLogs)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = () => resolve(history);
        }),
    )
    .mockRejectedValueOnce(new Error("offline"));
  const view = render(<StepLogPanel buildID="one" stepIndex={0} />);
  view.rerender(<StepLogPanel buildID="one" stepIndex={1} />);
  await screen.findByText(/Failed to load logs: Error: offline/);
  await act(async () => finish());
  expect(FakeStream.instances).toHaveLength(0);
});
