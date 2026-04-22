import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createSourceCredential,
  deleteSourceCredential,
  listSourceCredentials,
} from "../api";
import type { SourceCredentialKind } from "../types/managedImageSettings";
import { formatTime } from "../utils/time";

export function CredentialsPage() {
  const queryClient = useQueryClient();
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [credentialName, setCredentialName] = useState("");
  const [credentialKind, setCredentialKind] =
    useState<SourceCredentialKind>("https_token");
  const [credentialUsername, setCredentialUsername] =
    useState("x-access-token");
  const [credentialSecretRef, setCredentialSecretRef] = useState("");

  const {
    data: credentials,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["source-credentials"],
    queryFn: () => listSourceCredentials(),
  });

  const createMutation = useMutation({
    mutationFn: createSourceCredential,
    onMutate: () => setErrorMessage(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["source-credentials"] });
      setCredentialName("");
      setCredentialSecretRef("");
    },
    onError: (mutationError) => setErrorMessage(String(mutationError)),
  });

  const deleteMutation = useMutation({
    mutationFn: deleteSourceCredential,
    onMutate: () => setErrorMessage(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["source-credentials"] });
    },
    onError: (mutationError) => setErrorMessage(String(mutationError)),
  });

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!credentialName.trim() || !credentialSecretRef.trim()) {
      setErrorMessage("Credential name and secret ref are required.");
      return;
    }

    createMutation.mutate({
      name: credentialName.trim(),
      kind: credentialKind,
      username:
        credentialKind === "https_token" ? credentialUsername.trim() : "",
      secret_ref: credentialSecretRef.trim(),
    });
  };

  return (
    <>
      <h2>Credentials</h2>
      <p className="subtle-text">
        Admin-managed reusable Git write credentials for job-owned managed build
        image automation.
      </p>

      {errorMessage && <p className="error-text">{errorMessage}</p>}

      <section className="settings-panel" style={{ marginTop: 14 }}>
        <h3>Add Credential</h3>
        <form className="job-form" onSubmit={onSubmit}>
          <label htmlFor="credential-name">Name</label>
          <input
            id="credential-name"
            value={credentialName}
            onChange={(event) => setCredentialName(event.target.value)}
            placeholder="github-write-token"
            disabled={createMutation.isPending}
          />

          <label htmlFor="credential-kind">Kind</label>
          <select
            id="credential-kind"
            value={credentialKind}
            onChange={(event) =>
              setCredentialKind(event.target.value as SourceCredentialKind)
            }
            disabled={createMutation.isPending}
          >
            <option value="https_token">https_token</option>
            <option value="ssh_key">ssh_key</option>
          </select>

          <label htmlFor="credential-username">Username</label>
          <input
            id="credential-username"
            value={credentialUsername}
            onChange={(event) => setCredentialUsername(event.target.value)}
            placeholder="x-access-token"
            disabled={createMutation.isPending || credentialKind === "ssh_key"}
          />

          <label htmlFor="credential-secret-ref">Secret Ref</label>
          <input
            id="credential-secret-ref"
            value={credentialSecretRef}
            onChange={(event) => setCredentialSecretRef(event.target.value)}
            placeholder="GITHUB_WRITE_TOKEN"
            disabled={createMutation.isPending}
          />

          <div className="job-form-actions">
            <button type="submit" disabled={createMutation.isPending}>
              {createMutation.isPending ? "Saving…" : "Add Credential"}
            </button>
          </div>
        </form>
      </section>

      <section className="settings-panel" style={{ marginTop: 16 }}>
        <h3>Saved Credentials</h3>
        {isLoading && <p>Loading credentials…</p>}
        {error && (
          <p className="error-text">
            Failed to load credentials: {String(error)}
          </p>
        )}
        {credentials && credentials.length === 0 && (
          <p className="subtle-text">No credentials saved yet.</p>
        )}
        {credentials && credentials.length > 0 && (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Kind</th>
                <th>Secret Ref</th>
                <th>Updated</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {credentials.map((credential) => (
                <tr key={credential.id}>
                  <td>{credential.name}</td>
                  <td>{credential.kind}</td>
                  <td>{credential.secret_ref}</td>
                  <td>{formatTime(credential.updated_at)}</td>
                  <td>
                    <button
                      className="table-action-button"
                      type="button"
                      onClick={() => deleteMutation.mutate(credential.id)}
                      disabled={deleteMutation.isPending}
                    >
                      Remove
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
