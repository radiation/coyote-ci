import type { BuildStep } from "../types";
import { formatDuration, formatTime } from "../utils/time";
import { StatusBadge } from "./StatusBadge";

export function StepList({
  steps,
  selected,
  onSelect,
}: {
  steps: BuildStep[];
  selected?: BuildStep;
  onSelect: (index: number) => void;
}) {
  const groups: { name: string; steps: BuildStep[] }[] = [];
  for (const step of steps) {
    const name = step.group_name?.trim() ?? "";
    const last = groups.at(-1);
    if (last?.name === name) last.steps.push(step);
    else groups.push({ name, steps: [step] });
  }
  return (
    <nav className="investigation-nav" aria-label="Execution steps">
      {groups.map((group) => (
        <section
          key={group.steps[0].id}
          aria-label={group.name || "Sequential steps"}
        >
          <h4>
            {group.name || "Sequential"}
            {group.name && group.steps.length > 1 && (
              <small>Parallel · {group.steps.length}</small>
            )}
          </h4>
          {group.steps.map((step) => (
            <button
              key={step.id}
              type="button"
              aria-current={selected?.id === step.id ? "step" : undefined}
              onClick={() => onSelect(step.step_index)}
            >
              <span>
                {step.step_index + 1}. {step.name}
              </span>
              <StatusBadge status={step.status} />
              {step.started_at && step.finished_at && (
                <small>
                  {formatDuration(step.started_at, step.finished_at)}
                </small>
              )}
            </button>
          ))}
        </section>
      ))}
    </nav>
  );
}

export function StepDetails({ step }: { step: BuildStep }) {
  return (
    <>
      <pre className="investigation-command">{step.command}</pre>
      <dl className="investigation-facts">
        <dt>Status</dt>
        <dd>
          <StatusBadge status={step.status} />
        </dd>
        <dt>Worker</dt>
        <dd>{step.worker_id ?? "—"}</dd>
        <dt>Exit code</dt>
        <dd>{step.exit_code ?? "—"}</dd>
        <dt>Started</dt>
        <dd>{formatTime(step.started_at)}</dd>
        <dt>Finished</dt>
        <dd>{formatTime(step.finished_at)}</dd>
        <dt>Duration</dt>
        <dd>{formatDuration(step.started_at, step.finished_at)}</dd>
        {step.job?.timing?.phases.map((phase) => (
          <div className="investigation-phase" key={phase.name}>
            <dt>{phase.name.replace(/_/g, " ")}</dt>
            <dd>
              {formatDuration(
                phase.started_at ?? null,
                phase.finished_at ?? null,
              )}
            </dd>
          </div>
        ))}
      </dl>
      {step.error_message && <p className="error-text">{step.error_message}</p>}
    </>
  );
}
