import { useState, type FormEvent } from "react";
import type { VersionTag } from "../types";

interface VersionTagEditorProps {
  tags: VersionTag[];
  emptyText: string;
  inputLabel: string;
  submitLabel?: string;
  onAssign?: (version: string) => Promise<void>;
}

export function VersionTagEditor({
  tags,
  emptyText,
  inputLabel,
  submitLabel = "Assign",
  onAssign,
}: VersionTagEditorProps) {
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!onAssign) return;

    const trimmed = value.trim();
    if (!trimmed) {
      setError("Version is required.");
      return;
    }

    setIsSubmitting(true);
    setError(null);
    try {
      await onAssign(trimmed);
      setValue("");
    } catch (submitError) {
      setError(
        submitError instanceof Error
          ? submitError.message
          : String(submitError),
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
          <label className="sr-only" htmlFor={inputLabel}>
            {inputLabel}
          </label>
          <input
            id={inputLabel}
            value={value}
            onChange={(event) => setValue(event.target.value)}
            placeholder="2026.04.22"
            disabled={isSubmitting}
          />
          <button type="submit" disabled={isSubmitting}>
            {isSubmitting ? "Saving…" : submitLabel}
          </button>
        </form>
      )}

      {error && <p className="error-text">{error}</p>}
    </div>
  );
}
