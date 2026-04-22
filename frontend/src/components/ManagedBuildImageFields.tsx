import type { SourceCredential } from "../types/managedImageSettings";

export interface ManagedBuildImageValue {
  enabled: boolean;
  managedImageName: string;
  pipelinePath: string;
  writeCredentialID: string;
  botBranchPrefix: string;
  commitAuthorName: string;
  commitAuthorEmail: string;
}

interface ManagedBuildImageFieldsProps {
  value: ManagedBuildImageValue;
  onChange: (patch: Partial<ManagedBuildImageValue>) => void;
  credentials: SourceCredential[];
  disabled?: boolean;
  credentialsLoading?: boolean;
}

export function ManagedBuildImageFields({
  value,
  onChange,
  credentials,
  disabled = false,
  credentialsLoading = false,
}: ManagedBuildImageFieldsProps) {
  const statusLabel = value.enabled ? "Automation enabled" : "Automation off";

  return (
    <fieldset className="managed-image-section" disabled={disabled}>
      <legend>Managed Build Image</legend>

      <div className="managed-image-header">
        <div>
          <p className="managed-image-title">Managed image automation</p>
          <p className="subtle-text managed-image-description">
            Jobs own this automation. When enabled, Coyote CI refreshes the
            pipeline image and opens a write-back branch using the selected
            credential.
          </p>
        </div>
        <span
          className={`managed-image-status ${
            value.enabled ? "is-enabled" : "is-disabled"
          }`}
        >
          {statusLabel}
        </span>
      </div>

      <label
        className="checkbox-label managed-image-toggle"
        htmlFor="managed-image-enabled"
      >
        <input
          id="managed-image-enabled"
          type="checkbox"
          checked={value.enabled}
          onChange={(event) => onChange({ enabled: event.target.checked })}
        />
        Enable managed build image automation
      </label>

      {!value.enabled && (
        <div className="managed-image-empty-state">
          <p className="subtle-text">
            Turn this on when you want Coyote CI to keep the pipeline image
            pinned to a managed version for this job.
          </p>
        </div>
      )}

      {value.enabled && (
        <div className="managed-image-body">
          <div className="managed-image-group">
            <h4>Image Source</h4>
            <p className="subtle-text">
              Define what image should be managed and where Coyote CI should
              write the updated pipeline reference.
            </p>

            <div className="managed-image-grid">
              <div>
                <label htmlFor="managed-image-name">Managed Image Name</label>
                <input
                  id="managed-image-name"
                  value={value.managedImageName}
                  onChange={(event) =>
                    onChange({ managedImageName: event.target.value })
                  }
                  placeholder="go"
                />
              </div>

              <div>
                <label htmlFor="managed-image-pipeline-path">
                  Pipeline Path
                </label>
                <input
                  id="managed-image-pipeline-path"
                  value={value.pipelinePath}
                  onChange={(event) =>
                    onChange({ pipelinePath: event.target.value })
                  }
                  placeholder=".coyote/pipeline.yml"
                />
              </div>
            </div>

            <label htmlFor="managed-image-write-credential">
              Write Credential
            </label>
            <select
              id="managed-image-write-credential"
              value={value.writeCredentialID}
              onChange={(event) =>
                onChange({ writeCredentialID: event.target.value })
              }
            >
              <option value="">
                {credentialsLoading
                  ? "Loading credentials…"
                  : "Select credential"}
              </option>
              {credentials.map((credential) => (
                <option key={credential.id} value={credential.id}>
                  {credential.name} ({credential.kind})
                </option>
              ))}
            </select>
          </div>

          <div className="managed-image-group managed-image-group-secondary">
            <h4>Write-Back Defaults</h4>
            <p className="subtle-text">
              These values control the bot branch naming and commit identity
              used for automated refreshes.
            </p>

            <label htmlFor="managed-image-branch-prefix">
              Bot Branch Prefix
            </label>
            <input
              id="managed-image-branch-prefix"
              value={value.botBranchPrefix}
              onChange={(event) =>
                onChange({ botBranchPrefix: event.target.value })
              }
              placeholder="coyote/managed-image-refresh"
            />

            <div className="managed-image-grid">
              <div>
                <label htmlFor="managed-image-author-name">
                  Commit Author Name
                </label>
                <input
                  id="managed-image-author-name"
                  value={value.commitAuthorName}
                  onChange={(event) =>
                    onChange({ commitAuthorName: event.target.value })
                  }
                  placeholder="Coyote CI Bot"
                />
              </div>

              <div>
                <label htmlFor="managed-image-author-email">
                  Commit Author Email
                </label>
                <input
                  id="managed-image-author-email"
                  value={value.commitAuthorEmail}
                  onChange={(event) =>
                    onChange({ commitAuthorEmail: event.target.value })
                  }
                  placeholder="bot@coyote-ci.local"
                />
              </div>
            </div>
          </div>
        </div>
      )}
    </fieldset>
  );
}
