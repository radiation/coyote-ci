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
  return (
    <fieldset disabled={disabled}>
      <legend>Managed Build Image</legend>

      <label className="checkbox-label" htmlFor="managed-image-enabled">
        <input
          id="managed-image-enabled"
          type="checkbox"
          checked={value.enabled}
          onChange={(event) => onChange({ enabled: event.target.checked })}
        />
        Enable managed build image automation
      </label>

      <p className="subtle-text">
        Jobs own this automation. When enabled, Coyote CI can refresh the
        pipeline image using the selected write credential.
      </p>

      <label htmlFor="managed-image-name">Managed Image Name</label>
      <input
        id="managed-image-name"
        value={value.managedImageName}
        onChange={(event) => onChange({ managedImageName: event.target.value })}
        placeholder="go"
      />

      <label htmlFor="managed-image-pipeline-path">Pipeline Path</label>
      <input
        id="managed-image-pipeline-path"
        value={value.pipelinePath}
        onChange={(event) => onChange({ pipelinePath: event.target.value })}
        placeholder=".coyote/pipeline.yml"
      />

      <label htmlFor="managed-image-write-credential">Write Credential</label>
      <select
        id="managed-image-write-credential"
        value={value.writeCredentialID}
        onChange={(event) =>
          onChange({ writeCredentialID: event.target.value })
        }
      >
        <option value="">
          {credentialsLoading ? "Loading credentials…" : "Select credential"}
        </option>
        {credentials.map((credential) => (
          <option key={credential.id} value={credential.id}>
            {credential.name} ({credential.kind})
          </option>
        ))}
      </select>

      <label htmlFor="managed-image-branch-prefix">Bot Branch Prefix</label>
      <input
        id="managed-image-branch-prefix"
        value={value.botBranchPrefix}
        onChange={(event) => onChange({ botBranchPrefix: event.target.value })}
        placeholder="coyote/managed-image-refresh"
      />

      <label htmlFor="managed-image-author-name">Commit Author Name</label>
      <input
        id="managed-image-author-name"
        value={value.commitAuthorName}
        onChange={(event) => onChange({ commitAuthorName: event.target.value })}
        placeholder="Coyote CI Bot"
      />

      <label htmlFor="managed-image-author-email">Commit Author Email</label>
      <input
        id="managed-image-author-email"
        value={value.commitAuthorEmail}
        onChange={(event) =>
          onChange({ commitAuthorEmail: event.target.value })
        }
        placeholder="bot@coyote-ci.local"
      />
    </fieldset>
  );
}
