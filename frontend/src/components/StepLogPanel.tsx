import { useEffect, useState } from "react";
import { buildStepLogStreamURL, getStepLogs } from "../api";
import type { StepLogChunk } from "../types/build";

type Props = { buildID: string; stepIndex: number };

export function StepLogPanel(props: Props) {
  return (
    <StepLogSession key={`${props.buildID}:${props.stepIndex}`} {...props} />
  );
}

function StepLogSession({ buildID, stepIndex }: Props) {
  const [chunks, setChunks] = useState<StepLogChunk[] | null>(null);
  const [error, setError] = useState<string>();
  useEffect(() => {
    let closed = false;
    let source: EventSource | undefined;
    let timer: number | undefined;
    function connect(latestSequence: number) {
      const stream = new EventSource(
        buildStepLogStreamURL(buildID, stepIndex, latestSequence),
      );
      source = stream;
      stream.addEventListener("chunk", (event: MessageEvent) => {
        if (closed || stream !== source) return;
        const chunk: StepLogChunk = JSON.parse(event.data);
        if (chunk.sequence_no <= latestSequence) return;
        latestSequence = chunk.sequence_no;
        setChunks((previous) => [...(previous ?? []), chunk]);
      });
      stream.onerror = () => {
        if (closed || stream !== source) return;
        stream.close();
        source = undefined;
        timer = window.setTimeout(() => connect(latestSequence), 1500);
      };
    }
    async function bootstrap() {
      try {
        const history = await getStepLogs(buildID, stepIndex, 0, 500);
        if (closed) return;
        setChunks(history.chunks);
        connect(history.next_sequence);
      } catch (cause) {
        if (!closed) setError(String(cause));
      }
    }
    void bootstrap();
    return () => {
      closed = true;
      source?.close();
      window.clearTimeout(timer);
    };
  }, [buildID, stepIndex]);
  if (error) return <p className="error-text">Failed to load logs: {error}</p>;
  if (!chunks) return <p className="subtle-text">Loading logs...</p>;
  if (!chunks.length) return <p className="subtle-text">No logs yet.</p>;
  return (
    <pre className="step-log-pre">
      {chunks
        .map((chunk) => `[${chunk.stream}] ${chunk.chunk_text}`)
        .join("\n")}
    </pre>
  );
}
