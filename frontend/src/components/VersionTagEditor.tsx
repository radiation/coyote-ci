import { useId, useState, type FormEvent } from "react";
import type { VersionTag } from "../types";

export interface VersionTagEditorState {
  value: string;
  error: string | null;
  notice: string;
  isSubmitting: boolean;
}

const initialEditorState: VersionTagEditorState = {
  value: "",
  error: null,
  notice: "",
  isSubmitting: false,
};

interface VersionTagEditorProps {
  tags: VersionTag[];
  emptyText: string;
  inputLabel: string;
  submitLabel?: string;
  placeholder?: string;
  requiredMessage?: string;
  confirmAssignment?: (value: string) => boolean;
  onAssign?: (version: string) => Promise<void>;
  editor?: {
    state: Partial<VersionTagEditorState>;
    update: (patch: Partial<VersionTagEditorState>) => void;
  };
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
  editor,
}: VersionTagEditorProps) {
  const inputId = useId();
  const [localState, setLocalState] = useState(initialEditorState);
  const { value, error, notice, isSubmitting } = editor
    ? { ...initialEditorState, ...editor.state }
    : localState;
  const update =
    editor?.update ??
    ((patch: Partial<VersionTagEditorState>) =>
      setLocalState((current) => ({ ...current, ...patch })));

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!onAssign || isSubmitting) return;

    const trimmed = value.trim();
    if (!trimmed) {
      update({ notice: "", error: requiredMessage });
      return;
    }

    if (confirmAssignment && !confirmAssignment(trimmed)) return;
    update({ isSubmitting: true, notice: "", error: null });
    try {
      await onAssign(trimmed);
      update({
        notice: `Assigned ${trimmed}.`,
        value: "",
        isSubmitting: false,
      });
    } catch (submitError) {
      const status =
        submitError instanceof Error && "status" in submitError
          ? submitError.status
          : undefined;
      const detail =
        submitError instanceof Error
          ? submitError.message
          : String(submitError);
      update({
        isSubmitting: false,
        error:
          status === 403
            ? "Permission denied for this assignment."
            : status === 404
              ? "The assignment target was not found."
              : status === 409
                ? `Assignment conflict: ${detail}`
                : status === 400
                  ? `Invalid assignment: ${detail}`
                  : `Assignment failed: ${detail}`,
      });
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
            onChange={(event) => update({ value: event.target.value })}
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
