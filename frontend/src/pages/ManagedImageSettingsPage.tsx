import { useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createRepoWritebackConfig,
  createSourceCredential,
  deleteRepoWritebackConfig,
  deleteSourceCredential,
  listRepoWritebackConfigs,
  listSourceCredentials,
  updateRepoWritebackConfig,
} from "../api";
import type { SourceCredentialKind } from "../types/managedImageSettings";
import { formatTime } from "../utils/time";

export function ManagedImageSettingsPage() {
  const queryClient = useQueryClient();
  const [projectID, setProjectID] = useState("project-1");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const [credentialName, setCredentialName] = useState("");
  const [credentialKind, setCredentialKind] =
    useState<SourceCredentialKind>("https_token");
  const [credentialUsername, setCredentialUsername] =
    useState("x-access-token");
  const [credentialSecretRef, setCredentialSecretRef] = useState("");

  const [repositoryURL, setRepositoryURL] = useState("");
  const [pipelinePath, setPipelinePath] = useState(".coyote/pipeline.yml");
  const [managedImageName, setManagedImageName] = useState("go");
  const [writeCredentialID, setWriteCredentialID] = useState("");
  const [botBranchPrefix, setBotBranchPrefix] = useState(
    "coyote/managed-image-refresh",
  );
  const [commitAuthorName, setCommitAuthorName] = useState("Coyote CI Bot");
  const [commitAuthorEmail, setCommitAuthorEmail] = useState(
    "bot@coyote-ci.local",
  );
  const [writebackEnabled, setWritebackEnabled] = useState(true);

  const trimmedProjectID = projectID.trim();

  const {
    data: credentials,
    isLoading: credentialsLoading,
    error: credentialsError,
  } = useQuery({
    queryKey: ["source-credentials", trimmedProjectID],
    queryFn: () => listSourceCredentials(trimmedProjectID),
    enabled: trimmedProjectID.length > 0,
  });

  const {
    data: writebackConfigs,
    isLoading: writebackConfigsLoading,
    error: writebackConfigsError,
  } = useQuery({
    queryKey: ["repo-writeback-configs", trimmedProjectID],
    queryFn: () => listRepoWritebackConfigs(trimmedProjectID),
    enabled: trimmedProjectID.length > 0,
  });

  const creatingCredential = useMutation({
    mutationFn: createSourceCredential,
    onMutate: () => setErrorMessage(null),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({
        queryKey: ["source-credentials", trimmedProjectID],
      });
      setCredentialName("");
      setCredentialSecretRef("");
      setWriteCredentialID(created.id);
    },
    onError: (error) => setErrorMessage(String(error)),
  });

  const deletingCredential = useMutation({
    mutationFn: deleteSourceCredential,
    onMutate: () => setErrorMessage(null),
    onSuccess: async (_, credentialID) => {
      await queryClient.invalidateQueries({
        queryKey: ["source-credentials", trimmedProjectID],
      });
      if (writeCredentialID === credentialID) {
        setWriteCredentialID("");
      }
    },
    onError: (error) => setErrorMessage(String(error)),
  });

  const creatingWritebackConfig = useMutation({
    mutationFn: createRepoWritebackConfig,
    onMutate: () => setErrorMessage(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["repo-writeback-configs", trimmedProjectID],
      });
      setRepositoryURL("");
    },
    onError: (error) => setErrorMessage(String(error)),
  });

  const togglingWritebackConfig = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      updateRepoWritebackConfig(id, { enabled }),
    onMutate: () => setErrorMessage(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["repo-writeback-configs", trimmedProjectID],
      });
    },
    onError: (error) => setErrorMessage(String(error)),
  });

  const deletingWritebackConfig = useMutation({
    mutationFn: deleteRepoWritebackConfig,
    onMutate: () => setErrorMessage(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["repo-writeback-configs", trimmedProjectID],
      });
    },
    onError: (error) => setErrorMessage(String(error)),
  });

  const canCreateCredential = useMemo(() => {
    return (
      trimmedProjectID.length > 0 &&
      credentialName.trim().length > 0 &&
      credentialSecretRef.trim().length > 0
    );
  }, [trimmedProjectID, credentialName, credentialSecretRef]);

  const canCreateWritebackConfig = useMemo(() => {
    return (
      trimmedProjectID.length > 0 &&
      repositoryURL.trim().length > 0 &&
      pipelinePath.trim().length > 0 &&
      managedImageName.trim().length > 0 &&
      writeCredentialID.trim().length > 0
    );
  }, [
    trimmedProjectID,
    repositoryURL,
    pipelinePath,
    managedImageName,
    writeCredentialID,
  ]);

  const onCreateCredential = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canCreateCredential) {
      setErrorMessage("Project, credential name, and secret ref are required.");
      return;
    }

    creatingCredential.mutate({
      project_id: trimmedProjectID,
      name: credentialName.trim(),
      kind: credentialKind,
      username:
        credentialKind === "https_token" ? credentialUsername.trim() : "",
      secret_ref: credentialSecretRef.trim(),
    });
  };

  const onCreateWritebackConfig = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canCreateWritebackConfig) {
      setErrorMessage(
        "Project, repository URL, pipeline path, managed image name, and credential are required.",
      );
      return;
    }

    creatingWritebackConfig.mutate({
      project_id: trimmedProjectID,
      repository_url: repositoryURL.trim(),
      pipeline_path: pipelinePath.trim(),
      managed_image_name: managedImageName.trim(),
      write_credential_id: writeCredentialID.trim(),
      bot_branch_prefix: botBranchPrefix.trim(),
      commit_author_name: commitAuthorName.trim(),
      commit_author_email: commitAuthorEmail.trim(),
      enabled: writebackEnabled,
    });
  };

  return (
    <>
      <h2>Managed Image Settings</h2>
      <p className="subtle-text">
        Configure source credentials and repository write-back settings so
        managed image refresh can run without direct database edits.
      </p>

      <div className="job-form" style={{ marginTop: 12 }}>
        <label htmlFor="managed-project-id">Project ID</label>
        <input
          id="managed-project-id"
          value={projectID}
          onChange={(event) => setProjectID(event.target.value)}
          placeholder="project-1"
        />
      </div>

      {errorMessage && <p className="error-text">{errorMessage}</p>}

      <div className="settings-grid">
        <section className="settings-panel">
          <h3>Source Credentials</h3>
          <form className="job-form" onSubmit={onCreateCredential}>
            <label htmlFor="credential-name">Name</label>
            <input
              id="credential-name"
              value={credentialName}
              onChange={(event) => setCredentialName(event.target.value)}
              placeholder="github-write-token"
              disabled={creatingCredential.isPending}
            />

            <label htmlFor="credential-kind">Kind</label>
            <select
              id="credential-kind"
              value={credentialKind}
              onChange={(event) =>
                setCredentialKind(event.target.value as SourceCredentialKind)
              }
              disabled={creatingCredential.isPending}
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
              disabled={
                creatingCredential.isPending || credentialKind === "ssh_key"
              }
            />

            <label htmlFor="credential-secret-ref">Secret Ref</label>
            <input
              id="credential-secret-ref"
              value={credentialSecretRef}
              onChange={(event) => setCredentialSecretRef(event.target.value)}
              placeholder="GITHUB_WRITE_TOKEN"
              disabled={creatingCredential.isPending}
            />

            <div className="job-form-actions">
              <button
                type="submit"
                disabled={creatingCredential.isPending || !canCreateCredential}
              >
                {creatingCredential.isPending ? "Saving…" : "Add Credential"}
              </button>
            </div>
          </form>

          {credentialsLoading && <p>Loading credentials…</p>}
          {credentialsError && (
            <p className="error-text">
              Failed to load credentials: {String(credentialsError)}
            </p>
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
                        onClick={() => deletingCredential.mutate(credential.id)}
                        disabled={deletingCredential.isPending}
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

        <section className="settings-panel">
          <h3>Repo Write-Back Configs</h3>
          <form className="job-form" onSubmit={onCreateWritebackConfig}>
            <label htmlFor="writeback-repo-url">Repository URL</label>
            <input
              id="writeback-repo-url"
              value={repositoryURL}
              onChange={(event) => setRepositoryURL(event.target.value)}
              placeholder="https://github.com/org/repo.git"
              disabled={creatingWritebackConfig.isPending}
            />

            <label htmlFor="writeback-pipeline-path">Pipeline Path</label>
            <input
              id="writeback-pipeline-path"
              value={pipelinePath}
              onChange={(event) => setPipelinePath(event.target.value)}
              disabled={creatingWritebackConfig.isPending}
            />

            <label htmlFor="writeback-managed-image-name">
              Managed Image Name
            </label>
            <input
              id="writeback-managed-image-name"
              value={managedImageName}
              onChange={(event) => setManagedImageName(event.target.value)}
              placeholder="go"
              disabled={creatingWritebackConfig.isPending}
            />

            <label htmlFor="writeback-credential-id">Write Credential</label>
            <select
              id="writeback-credential-id"
              value={writeCredentialID}
              onChange={(event) => setWriteCredentialID(event.target.value)}
              disabled={creatingWritebackConfig.isPending}
            >
              <option value="">Select credential</option>
              {(credentials ?? []).map((credential) => (
                <option key={credential.id} value={credential.id}>
                  {credential.name} ({credential.kind})
                </option>
              ))}
            </select>

            <label htmlFor="writeback-branch-prefix">Bot Branch Prefix</label>
            <input
              id="writeback-branch-prefix"
              value={botBranchPrefix}
              onChange={(event) => setBotBranchPrefix(event.target.value)}
              disabled={creatingWritebackConfig.isPending}
            />

            <label htmlFor="writeback-author-name">Commit Author Name</label>
            <input
              id="writeback-author-name"
              value={commitAuthorName}
              onChange={(event) => setCommitAuthorName(event.target.value)}
              disabled={creatingWritebackConfig.isPending}
            />

            <label htmlFor="writeback-author-email">Commit Author Email</label>
            <input
              id="writeback-author-email"
              value={commitAuthorEmail}
              onChange={(event) => setCommitAuthorEmail(event.target.value)}
              disabled={creatingWritebackConfig.isPending}
            />

            <label className="checkbox-label" htmlFor="writeback-enabled">
              <input
                id="writeback-enabled"
                type="checkbox"
                checked={writebackEnabled}
                onChange={(event) => setWritebackEnabled(event.target.checked)}
                disabled={creatingWritebackConfig.isPending}
              />
              Enabled
            </label>

            <div className="job-form-actions">
              <button
                type="submit"
                disabled={
                  creatingWritebackConfig.isPending || !canCreateWritebackConfig
                }
              >
                {creatingWritebackConfig.isPending
                  ? "Saving…"
                  : "Add Write-Back Config"}
              </button>
            </div>
          </form>

          {writebackConfigsLoading && <p>Loading write-back configs…</p>}
          {writebackConfigsError && (
            <p className="error-text">
              Failed to load write-back configs: {String(writebackConfigsError)}
            </p>
          )}
          {writebackConfigs && writebackConfigs.length > 0 && (
            <table className="table">
              <thead>
                <tr>
                  <th>Repository</th>
                  <th>Pipeline</th>
                  <th>Image Name</th>
                  <th>Enabled</th>
                  <th>Updated</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {writebackConfigs.map((cfg) => (
                  <tr key={cfg.id}>
                    <td>{cfg.repository_url}</td>
                    <td>{cfg.pipeline_path}</td>
                    <td>{cfg.managed_image_name}</td>
                    <td>{cfg.enabled ? "Enabled" : "Disabled"}</td>
                    <td>{formatTime(cfg.updated_at)}</td>
                    <td>
                      <div className="table-actions">
                        <button
                          className="table-action-button"
                          type="button"
                          onClick={() =>
                            togglingWritebackConfig.mutate({
                              id: cfg.id,
                              enabled: !cfg.enabled,
                            })
                          }
                          disabled={togglingWritebackConfig.isPending}
                        >
                          {cfg.enabled ? "Disable" : "Enable"}
                        </button>
                        <button
                          className="table-action-button"
                          type="button"
                          onClick={() => deletingWritebackConfig.mutate(cfg.id)}
                          disabled={deletingWritebackConfig.isPending}
                        >
                          Remove
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      </div>
    </>
  );
}
