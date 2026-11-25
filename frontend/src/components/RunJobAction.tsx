import { useMutation } from "@tanstack/react-query";
import { useRef } from "react";
import { useNavigate } from "react-router-dom";
import { runJob } from "../api";

export function RunJobAction({
  jobID,
  jobName,
}: {
  jobID: string;
  jobName?: string;
}) {
  const navigate = useNavigate();
  const submitted = useRef(false);
  const run = useMutation({
    mutationFn: () => runJob(jobID),
    retry: false,
    onSettled: () => {
      submitted.current = false;
    },
  });

  return (
    <div>
      <button
        type="button"
        className="table-action-button"
        aria-label={jobName ? `Run Now: ${jobName}` : undefined}
        aria-disabled={run.isPending}
        onClick={() => {
          if (submitted.current) return;
          submitted.current = true;
          run.mutate(undefined, {
            onSuccess: (build) => {
              const id = build.id?.trim();
              if (id) navigate(`/builds/${encodeURIComponent(id)}`);
            },
          });
        }}
      >
        {run.isPending ? "Running…" : "Run Now"}
      </button>
      {run.isError && (
        <p role="alert" className="error-text">
          Failed to run job: {String(run.error)}
        </p>
      )}
      {run.isSuccess && !run.data.id?.trim() && (
        <p role="status">Job run started. No build link was returned.</p>
      )}
    </div>
  );
}
