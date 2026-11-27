import { useId, useState, type FormEvent } from "react";
import type { VersionTag } from "../types";

interface VersionTagEditorProps {
  tags: VersionTag[];
  emptyText: string;
  inputLabel: string;
  submitLabel?: string;
  placeholder?: string;
  requiredMessage?: string;
  confirmAssignment?: (value: string) => boolean;
  onAssign?: (version: string) => Promise<void>;
}

export function VersionTagEditor({
  tags,
  emptyText,
  inputLabel,
  submitLabel = "Assign",
  placeholder = "2026.04.22",
  requiredMessage = "Value is required.",
  confirmAssignment,
  onAssign,
}: VersionTagEditorProps) {
  const inputId = useId();
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!onAssign || isSubmitting) return;

    const trimmed = value.trim();
    setNotice("");
    if (!trimmed) {
      setError(requiredMessage);
      return;
    }

    if (confirmAssignment && !confirmAssignment(trimmed)) return;
    setIsSubmitting(true);
    setError(null);
    try {
      await onAssign(trimmed);
      setNotice(`Assigned ${trimmed}.`);
      setValue("");
    } catch (submitError) {
      const status =
        submitError instanceof Error && "status" in submitError
          ? submitError.status
          : undefined;
      const detail =
        submitError instanceof Error
          ? submitError.message
          : String(submitError);
      setError(
        status === 403
          ? "Permission denied for this assignment."
          : status === 404
            ? "The assignment target was not found."
            : status === 409
              ? `Assignment conflict: ${detail}`
              : status === 400
                ? `Invalid assignment: ${detail}`
                : `Assignment failed: ${detail}`,
      );
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <div className="version-tag-editor">
      <div className="version-tag-list" aria-label={`${inputLabel} tags`}>
        {tags.length > 0 ? (
          tags.map((tag) => (
            <span key={tag.id} className="version-tag-pill">
              {tag.version}
            </span>
          ))
        ) : (
          <span className="subtle-text">{emptyText}</span>
        )}
      </div>

      {onAssign && (
        <form className="version-tag-form" onSubmit={handleSubmit}>
          <label className="sr-only" htmlFor={inputId}>
            {inputLabel}
          </label>
          <input
            id={inputId}
            value={value}
            onChange={(event) => setValue(event.target.value)}
            placeholder={placeholder}
            disabled={isSubmitting}
          />
          <button
            type="submit"
            className="inline-action-button version-tag-submit-button"
            disabled={isSubmitting}
          >
            {isSubmitting ? "Saving…" : submitLabel}
          </button>
        </form>
      )}

      <span role="status">{notice}</span>
      {error && (
        <p role="alert" className="error-text">
          {error}
        </p>
      )}
    </div>
  );
}
