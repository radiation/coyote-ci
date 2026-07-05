import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createAPIToken,
  formatAPIErrorMessage,
  isAPIErrorStatus,
  listAPITokens,
  revokeAPIToken,
} from "../api";
import { useAuth } from "../auth-context";
import type { APITokenScope, CreatedAPIToken } from "../types/identity";
import { formatTime } from "../utils/time";

const TOKEN_COPY_STATUS_RESET_MS = 2000;

const TOKEN_SCOPE_OPTIONS: Array<{
  value: APITokenScope;
  label: string;
  description: string;
}> = [
  {
    value: "build:read",
    label: "Build status",
    description: "Read build metadata, status, and project or job context.",
  },
  {
    value: "build:logs",
    label: "Build logs",
    description: "Read build logs and failed-step diagnostics.",
  },
  {
    value: "build:run",
    label: "Rerun builds",
    description: "Trigger reruns and other build-run actions.",
  },
  {
    value: "artifact:read",
    label: "Read artifacts",
    description: "List artifact metadata and download build artifacts.",
  },
];

const DEFAULT_TOKEN_SCOPES: APITokenScope[] = ["build:read", "build:logs"];

export function APITokensPage() {
  const { authMode } = useAuth();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [selectedScopes, setSelectedScopes] =
    useState<APITokenScope[]>(DEFAULT_TOKEN_SCOPES);
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
    enabled: authMode !== "disabled",
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
      setSelectedScopes(DEFAULT_TOKEN_SCOPES);
      await queryClient.invalidateQueries({ queryKey: ["api-tokens"] });
    },
    onError: (mutationError) => {
      setErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "API tokens can only be managed from a signed-in user session.",
          "Failed to create API token",
        ),
      );
    },
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
    onError: async (mutationError, tokenID) => {
      if (isAPIErrorStatus(mutationError, 404)) {
        if (createdToken?.id === tokenID) {
          setCreatedToken(null);
        }
        await queryClient.invalidateQueries({ queryKey: ["api-tokens"] });
        return;
      }

      setErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "API tokens can only be managed from a signed-in user session.",
          "Failed to revoke API token",
        ),
      );
    },
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
      scopes: selectedScopes,
    });
  }

  function toggleScope(scope: APITokenScope) {
    setSelectedScopes((current) => {
      if (current.includes(scope)) {
        return current.filter((value) => value !== scope);
      }
      return [...current, scope].sort();
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

  function dismissCreatedToken() {
    setCreatedToken(null);
    setCopyStatus("idle");
  }

  function confirmRevoke(tokenName: string, tokenPrefix: string): boolean {
    return window.confirm(`Revoke API token ${tokenName} (${tokenPrefix})?`);
  }

  if (authMode === "disabled") {
    return (
      <>
        <div className="page-header-row">
          <div>
            <h2>My API Tokens</h2>
            <p className="subtle-text">
              Personal tokens for the Coyote CLI and other authenticated API
              clients.
            </p>
          </div>
        </div>

        <section className="settings-panel" style={{ marginTop: 14 }}>
          <h3>Unavailable in disabled auth mode</h3>
          <p className="subtle-text">
            API tokens require an authenticated user identity. Disabled auth
            mode uses a synthetic local-development identity, so token
            management is not available.
          </p>
        </section>
      </>
    );
  }

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>My API Tokens</h2>
          <p className="subtle-text">
            Personal tokens for the Coyote CLI and other authenticated API
            clients.
          </p>
        </div>
      </div>

      {errorMessage && <p className="error-text">{errorMessage}</p>}

      <section className="settings-panel" style={{ marginTop: 14 }}>
        <h3>Create Personal Token</h3>
        <form className="job-form" onSubmit={onSubmit}>
          <label htmlFor="api-token-name">Name</label>
          <input
            id="api-token-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="coyote cli"
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
          <p className="subtle-text api-token-form-copy">
            Leave expiration blank to create a token that does not expire.
          </p>

          <fieldset className="job-form-fieldset">
            <legend>Scopes</legend>
            <p className="subtle-text api-token-form-copy">
              Choose the smallest set of permissions this token needs. The
              default is a read-only diagnostic token for IDE agents.
            </p>
            {TOKEN_SCOPE_OPTIONS.map((scope) => (
              <label key={scope.value}>
                <input
                  type="checkbox"
                  checked={selectedScopes.includes(scope.value)}
                  onChange={() => toggleScope(scope.value)}
                  disabled={createMutation.isPending}
                />
                <span>{scope.label}</span>
                <span className="subtle-text"> {scope.description}</span>
              </label>
            ))}
          </fieldset>

          <div className="job-form-actions">
            <button type="submit" disabled={createMutation.isPending}>
              {createMutation.isPending ? "Creating..." : "Create Token"}
            </button>
          </div>
        </form>
      </section>

      {createdToken && (
        <section className="settings-panel api-token-created-panel">
          <h3>Copy Your New Token Now</h3>
          <p className="subtle-text">
            Copy this token now. Coyote will not show it again.
          </p>
          <textarea
            readOnly
            value={createdToken.token}
            aria-label="New token"
            rows={3}
            className="api-token-created-secret"
          />
          <div className="api-token-created-actions">
            <button
              type="button"
              className="table-action-button"
              onClick={() => void copyToken()}
            >
              Copy token
            </button>
            <button
              type="button"
              className="table-action-button"
              onClick={dismissCreatedToken}
            >
              Done
            </button>
          </div>
          <p className="subtle-text api-token-command-copy">Next CLI step</p>
          <code className="api-token-command">coyote auth token set</code>
          {copyStatus === "copied" && (
            <p className="subtle-text api-token-copy-status" aria-live="polite">
              Token copied.
            </p>
          )}
          {copyStatus === "failed" && (
            <p className="error-text api-token-copy-status" aria-live="polite">
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
          <p className="subtle-text">
            No personal API tokens yet. Create one here to use the Coyote CLI or
            another authenticated API client without relying on a browser
            session.
          </p>
        )}
        {tokens && tokens.length > 0 && (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Prefix</th>
                <th>Scopes</th>
                <th>Created</th>
                <th>Last Used</th>
                <th>Expires</th>
                <th>Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {tokens.map((token) => {
                const isRevoked = Boolean(token.revoked_at);

                return (
                  <tr key={token.id}>
                    <td>{token.name}</td>
                    <td>
                      <code>{token.token_prefix}</code>
                    </td>
                    <td>
                      {token.scopes.length > 0
                        ? token.scopes.join(", ")
                        : "Identity only"}
                    </td>
                    <td>{formatTime(token.created_at)}</td>
                    <td>
                      {token.last_used_at
                        ? formatTime(token.last_used_at)
                        : "-"}
                    </td>
                    <td>
                      {token.expires_at
                        ? formatTime(token.expires_at)
                        : "Never"}
                    </td>
                    <td>{isRevoked ? "Revoked" : "Active"}</td>
                    <td>
                      <button
                        type="button"
                        className="table-action-button"
                        onClick={() => {
                          if (!confirmRevoke(token.name, token.token_prefix)) {
                            return;
                          }
                          revokeMutation.mutate(token.id);
                        }}
                        disabled={revokeMutation.isPending || isRevoked}
                      >
                        {isRevoked ? "Revoked" : "Revoke"}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}
