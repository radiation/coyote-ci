import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createAPIToken,
  formatAPIErrorMessage,
  listAPITokens,
  revokeAPIToken,
} from "../api";
import type { CreatedAPIToken } from "../types/identity";
import { formatTime } from "../utils/time";

const TOKEN_COPY_STATUS_RESET_MS = 2000;

export function APITokensPage() {
  const queryClient = useQueryClient();
  const [name, setName] = useState("fixture populator");
  const [expiresAt, setExpiresAt] = useState("");
  const [createdToken, setCreatedToken] = useState<CreatedAPIToken | null>(
    null,
  );
  const [copyStatus, setCopyStatus] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const {
    data: tokens,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["api-tokens"],
    queryFn: listAPITokens,
  });

  const createMutation = useMutation({
    mutationFn: createAPIToken,
    onMutate: () => {
      setCreatedToken(null);
      setErrorMessage(null);
      setCopyStatus("idle");
    },
    onSuccess: async (token) => {
      setCreatedToken(token);
      setName("");
      setExpiresAt("");
      await queryClient.invalidateQueries({ queryKey: ["api-tokens"] });
    },
    onError: (mutationError) =>
      setErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "API tokens can only be managed from a signed-in user session.",
          "Failed to create API token",
        ),
      ),
  });

  const revokeMutation = useMutation({
    mutationFn: revokeAPIToken,
    onMutate: () => setErrorMessage(null),
    onSuccess: async (_data, tokenID) => {
      if (createdToken?.id === tokenID) {
        setCreatedToken(null);
      }
      await queryClient.invalidateQueries({ queryKey: ["api-tokens"] });
    },
    onError: (mutationError) =>
      setErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "API tokens can only be managed from a signed-in user session.",
          "Failed to revoke API token",
        ),
      ),
  });

  useEffect(() => {
    if (copyStatus === "idle") {
      return;
    }
    const timer = window.setTimeout(
      () => setCopyStatus("idle"),
      TOKEN_COPY_STATUS_RESET_MS,
    );
    return () => window.clearTimeout(timer);
  }, [copyStatus]);

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedName = name.trim();
    if (!trimmedName) {
      setErrorMessage("Token name is required.");
      return;
    }

    createMutation.mutate({
      name: trimmedName,
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : undefined,
    });
  }

  async function copyToken() {
    if (!createdToken) {
      return;
    }
    try {
      if (!globalThis.navigator?.clipboard?.writeText) {
        throw new Error("Clipboard API unavailable");
      }
      await globalThis.navigator.clipboard.writeText(createdToken.token);
      setCopyStatus("copied");
    } catch {
      setCopyStatus("failed");
    }
  }

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>API Tokens</h2>
          <p className="subtle-text">
            User-owned tokens for scripts, fixtures, and CLI access.
          </p>
        </div>
      </div>

      {errorMessage && <p className="error-text">{errorMessage}</p>}

      <section className="settings-panel" style={{ marginTop: 14 }}>
        <h3>Create Token</h3>
        <form className="job-form" onSubmit={onSubmit}>
          <label htmlFor="api-token-name">Name</label>
          <input
            id="api-token-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="fixture populator"
            disabled={createMutation.isPending}
          />

          <label htmlFor="api-token-expires-at">Expires At</label>
          <input
            id="api-token-expires-at"
            type="datetime-local"
            value={expiresAt}
            onChange={(event) => setExpiresAt(event.target.value)}
            disabled={createMutation.isPending}
          />

          <div className="job-form-actions">
            <button type="submit" disabled={createMutation.isPending}>
              {createMutation.isPending ? "Creating..." : "Create Token"}
            </button>
          </div>
        </form>
      </section>

      {createdToken && (
        <section className="settings-panel api-token-created-panel">
          <h3>New Token</h3>
          <p className="subtle-text">
            Copy this value now. Coyote will not show it again.
          </p>
          <div className="api-token-created-value">
            <input readOnly value={createdToken.token} aria-label="New token" />
            <button
              type="button"
              className="table-action-button"
              onClick={() => void copyToken()}
            >
              Copy token
            </button>
          </div>
          {copyStatus === "copied" && (
            <p className="subtle-text api-token-copy-status">Token copied.</p>
          )}
          {copyStatus === "failed" && (
            <p className="error-text api-token-copy-status">
              Unable to copy token.
            </p>
          )}
        </section>
      )}

      <section className="settings-panel" style={{ marginTop: 16 }}>
        <h3>Existing Tokens</h3>
        {isLoading && <p>Loading API tokens...</p>}
        {error && (
          <p className="error-text">
            {formatAPIErrorMessage(
              error,
              "API tokens can only be managed from a signed-in user session.",
              "Failed to load API tokens",
            )}
          </p>
        )}
        {tokens && tokens.length === 0 && (
          <p className="subtle-text">No API tokens have been created yet.</p>
        )}
        {tokens && tokens.length > 0 && (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Prefix</th>
                <th>Created</th>
                <th>Last Used</th>
                <th>Expires</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {tokens.map((token) => (
                <tr key={token.id}>
                  <td>{token.name}</td>
                  <td>
                    <code>{token.token_prefix}</code>
                  </td>
                  <td>{formatTime(token.created_at)}</td>
                  <td>
                    {token.last_used_at ? formatTime(token.last_used_at) : "-"}
                  </td>
                  <td>
                    {token.expires_at ? formatTime(token.expires_at) : "-"}
                  </td>
                  <td>
                    <button
                      type="button"
                      className="table-action-button"
                      onClick={() => revokeMutation.mutate(token.id)}
                      disabled={revokeMutation.isPending}
                    >
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}
