package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ApplicationReleaseRepository struct{ db *sql.DB }

func NewApplicationReleaseRepository(db *sql.DB) *ApplicationReleaseRepository {
	return &ApplicationReleaseRepository{db: db}
}

const applicationReleaseColumns = `id, build_id, project_id, job_id, application_version, status, auto_channel_name, failure_reason, created_at, completed_at, failed_at, updated_at`

func (r *ApplicationReleaseRepository) Create(ctx context.Context, release domain.ApplicationRelease) (domain.ApplicationRelease, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	defer func() { _ = tx.Rollback() }()
	created, err := scanApplicationRelease(tx.QueryRowContext(ctx, `INSERT INTO application_releases (`+applicationReleaseColumns+`) VALUES ($1,$2,$3,$4,$5,'pending',$6,NULL,$7,NULL,NULL,$7) RETURNING `+applicationReleaseColumns,
		release.ID, release.BuildID, release.ProjectID, release.JobID, release.ApplicationVersion, nullableTrimmedString(release.AutoChannelName), release.CreatedAt))
	if err != nil {
		if applicationReleaseUniqueViolation(err) {
			return domain.ApplicationRelease{}, repository.ErrApplicationReleaseConflict
		}
		return domain.ApplicationRelease{}, err
	}
	for _, output := range release.Outputs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_release_outputs (id, release_id, output_kind, output_key, display_name, definition_json, status, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6::jsonb,'pending',$7,$7)`,
			output.ID, created.ID, output.Kind, output.OutputKey, output.DisplayName, output.DefinitionJSON, release.CreatedAt); err != nil {
			return domain.ApplicationRelease{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.ApplicationRelease{}, err
	}
	return r.GetByID(ctx, created.ID)
}

func (r *ApplicationReleaseRepository) GetByBuildID(ctx context.Context, buildID string) (domain.ApplicationRelease, error) {
	return r.get(ctx, r.db, `SELECT `+applicationReleaseColumns+` FROM application_releases WHERE build_id=$1`, buildID)
}

func (r *ApplicationReleaseRepository) GetByID(ctx context.Context, releaseID string) (domain.ApplicationRelease, error) {
	return r.get(ctx, r.db, `SELECT `+applicationReleaseColumns+` FROM application_releases WHERE id=$1`, releaseID)
}

type applicationReleaseQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *ApplicationReleaseRepository) get(ctx context.Context, queryer applicationReleaseQueryer, query string, arg string) (domain.ApplicationRelease, error) {
	release, err := scanApplicationRelease(queryer.QueryRowContext(ctx, query, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
	}
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	outputs, err := r.listOutputs(ctx, release.ID)
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	release.Outputs = outputs
	return release, nil
}

func (r *ApplicationReleaseRepository) ListPending(ctx context.Context, limit int) ([]domain.ApplicationRelease, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+applicationReleaseColumns+` FROM application_releases WHERE status='pending' ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var releases []domain.ApplicationRelease
	for rows.Next() {
		release, scanErr := scanApplicationRelease(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		releases = append(releases, release)
	}
	return releases, rows.Err()
}

// Reconcile snapshots verified V2A/V2B publication records, then finalizes the
// release and automatic latest pointer in one transaction.
func (r *ApplicationReleaseRepository) Reconcile(ctx context.Context, releaseID string) (domain.ApplicationRelease, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	defer func() { _ = tx.Rollback() }()
	release, err := scanApplicationRelease(tx.QueryRowContext(ctx, `SELECT `+applicationReleaseColumns+` FROM application_releases WHERE id=$1 FOR UPDATE`, releaseID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
	}
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	if release.Status != domain.ApplicationReleaseStatusPending {
		if err = tx.Commit(); err != nil {
			return domain.ApplicationRelease{}, err
		}
		return r.GetByID(ctx, releaseID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, output_kind, output_key, display_name, definition_json::text, status, artifact_id, artifact_version_id, execution_job_id, immutable_reference, failure_reason, published_at, failed_at, created_at, updated_at FROM application_release_outputs WHERE release_id=$1 FOR UPDATE`, releaseID)
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	outputs := []domain.ApplicationReleaseOutput{}
	for rows.Next() {
		output, scanErr := scanApplicationReleaseOutput(rows)
		if scanErr != nil {
			_ = rows.Close()
			return domain.ApplicationRelease{}, scanErr
		}
		output.ReleaseID = releaseID
		outputs = append(outputs, output)
	}
	if err = rows.Close(); err != nil {
		return domain.ApplicationRelease{}, err
	}
	for _, output := range outputs {
		if output.Status == domain.ApplicationReleaseOutputPublished {
			continue
		}
		switch output.Kind {
		case domain.ApplicationReleaseOutputArtifact:
			_, err = tx.ExecContext(ctx, `UPDATE application_release_outputs o SET status='published', artifact_id=a.id, artifact_version_id=v.id, immutable_reference=v.version_text || ':' || a.id::text, published_at=NOW(), updated_at=NOW() FROM build_artifacts a JOIN artifact_versions v ON v.artifact_id=a.id WHERE o.id=$1 AND a.build_id=$2 AND a.step_id IS NULL AND a.logical_path=(o.definition_json->>'path') AND a.artifact_name=(o.definition_json->>'name') AND v.version_text=$3 AND v.artifact_id=a.id`, output.ID, release.BuildID, release.ApplicationVersion)
		case domain.ApplicationReleaseOutputImage:
			err = r.reconcileImageOutput(ctx, tx, release, output)
		}

		if err != nil {
			return domain.ApplicationRelease{}, err
		}
	}
	var incomplete bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM application_release_outputs WHERE release_id=$1 AND status <> 'published')`, releaseID).Scan(&incomplete); err != nil {
		return domain.ApplicationRelease{}, err
	}
	if !incomplete {
		now := time.Now().UTC()
		if _, err = tx.ExecContext(ctx, `UPDATE application_releases SET status='complete', completed_at=$2, updated_at=$2 WHERE id=$1`, releaseID, now); err != nil {
			return domain.ApplicationRelease{}, err
		}
		if strings.TrimSpace(release.AutoChannelName) == "latest" {
			if err = moveChannel(ctx, tx, release, "latest", nil, "automatic release completion"); err != nil {
				return domain.ApplicationRelease{}, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.ApplicationRelease{}, err
	}
	return r.GetByID(ctx, releaseID)
}

func (r *ApplicationReleaseRepository) reconcileImageOutput(ctx context.Context, tx *sql.Tx, release domain.ApplicationRelease, output domain.ApplicationReleaseOutput) error {
	rows, err := tx.QueryContext(ctx, `SELECT j.id, e.target_image_reference, e.published_image_reference, e.published_image_digest_reference FROM build_jobs j JOIN build_steps s ON s.id=j.step_id JOIN external_image_builds e ON e.execution_job_id=j.id WHERE j.build_id=$1 AND s.node_id=(($2::jsonb)->>'node_id') AND e.terminal_result='success'`, release.BuildID, output.DefinitionJSON)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var jobID, logicalTarget, publishedReference, digestReference string
		if scanErr := rows.Scan(&jobID, &logicalTarget, &publishedReference, &digestReference); scanErr != nil {
			return scanErr
		}
		if !imagePublicationMatches(output.DefinitionJSON, release.ApplicationVersion, logicalTarget, publishedReference, digestReference) {
			continue
		}
		_, updateErr := tx.ExecContext(ctx, `UPDATE application_release_outputs SET status='published', execution_job_id=$2, immutable_reference=$3, published_at=NOW(), updated_at=NOW() WHERE id=$1 AND status='pending'`, output.ID, jobID, digestReference)
		return updateErr
	}
	return rows.Err()
}

func imagePublicationMatches(definitionJSON string, version string, logicalTarget string, publishedReference string, digestReference string) bool {
	var definition struct {
		TargetImageReference string `json:"target_image_reference"`
	}
	if err := json.Unmarshal([]byte(definitionJSON), &definition); err != nil {
		return false
	}
	if strings.TrimSpace(logicalTarget) != strings.TrimSpace(definition.TargetImageReference) {
		return false
	}
	publishedRepository, publishedTag, ok := splitPublishedImageReference(publishedReference)
	if !ok || publishedTag != strings.TrimSpace(version) {
		return false
	}
	digestRepository, ok := splitDigestImageReference(digestReference)
	return ok && publishedRepository == digestRepository
}

func splitPublishedImageReference(reference string) (repository string, tag string, ok bool) {
	reference = strings.TrimSpace(reference)
	lastSlash := strings.LastIndex(reference, "/")
	lastColon := strings.LastIndex(reference, ":")
	if lastColon <= lastSlash || lastColon == len(reference)-1 || strings.Contains(reference, "@") {
		return "", "", false
	}
	return reference[:lastColon], reference[lastColon+1:], true
}

func splitDigestImageReference(reference string) (repository string, ok bool) {
	reference = strings.TrimSpace(reference)
	repository, digest, found := strings.Cut(reference, "@")
	if !found || strings.TrimSpace(repository) == "" || !strings.HasPrefix(digest, "sha256:") || len(digest) <= len("sha256:") {
		return "", false
	}
	return repository, true
}

func (r *ApplicationReleaseRepository) FailBuildRelease(ctx context.Context, buildID string, reason string) (domain.ApplicationRelease, error) {
	release, err := scanApplicationRelease(r.db.QueryRowContext(ctx, `UPDATE application_releases SET status='failed', failure_reason=$2, failed_at=NOW(), updated_at=NOW() WHERE build_id=$1 AND status='pending' RETURNING `+applicationReleaseColumns, buildID, strings.TrimSpace(reason)))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
	}
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	return r.GetByID(ctx, release.ID)
}

func (r *ApplicationReleaseRepository) PromoteStable(ctx context.Context, releaseID string, actorUserID *string) (domain.ApplicationRelease, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	defer func() { _ = tx.Rollback() }()
	release, err := scanApplicationRelease(tx.QueryRowContext(ctx, `SELECT `+applicationReleaseColumns+` FROM application_releases WHERE id=$1 FOR UPDATE`, releaseID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
	}
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	if release.Status != domain.ApplicationReleaseStatusComplete {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseIncomplete
	}
	if release.JobID == nil {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseChannelScope
	}
	if err = moveChannel(ctx, tx, release, "stable", actorUserID, "manual stable promotion"); err != nil {
		return domain.ApplicationRelease{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.ApplicationRelease{}, err
	}
	return r.GetByID(ctx, release.ID)
}

func moveChannel(ctx context.Context, tx *sql.Tx, release domain.ApplicationRelease, name string, actorUserID *string, reason string) error {
	var channelID, prior sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id, current_release_id FROM application_channels WHERE project_id=$1 AND job_id IS NOT DISTINCT FROM $2 AND channel_name=$3 FOR UPDATE`, release.ProjectID, release.JobID, name).Scan(&channelID, &prior)
	if errors.Is(err, sql.ErrNoRows) {
		channelID.String, channelID.Valid = uuid.NewString(), true
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_channels (id, project_id, job_id, channel_name, current_release_id, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,NOW(),NOW())`, channelID.String, release.ProjectID, release.JobID, name, release.ID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if prior.String == release.ID {
		return nil
	} else if _, err = tx.ExecContext(ctx, `UPDATE application_channels SET current_release_id=$2, updated_at=NOW() WHERE id=$1`, channelID.String, release.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO application_channel_events (id, channel_id, previous_release_id, new_release_id, actor_user_id, reason) VALUES ($1,$2,$3,$4,$5,$6)`, uuid.NewString(), channelID.String, nullableSQLString(prior), release.ID, actorUserID, reason)
	return err
}

func (r *ApplicationReleaseRepository) listOutputs(ctx context.Context, releaseID string) ([]domain.ApplicationReleaseOutput, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, output_kind, output_key, display_name, definition_json::text, status, artifact_id, artifact_version_id, execution_job_id, immutable_reference, failure_reason, published_at, failed_at, created_at, updated_at FROM application_release_outputs WHERE release_id=$1 ORDER BY output_key`, releaseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var outputs []domain.ApplicationReleaseOutput
	for rows.Next() {
		output, scanErr := scanApplicationReleaseOutput(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		output.ReleaseID = releaseID
		outputs = append(outputs, output)
	}
	return outputs, rows.Err()
}

func scanApplicationRelease(row interface{ Scan(...any) error }) (domain.ApplicationRelease, error) {
	var release domain.ApplicationRelease
	var jobID, autoChannel, reason sql.NullString
	var status string
	err := row.Scan(&release.ID, &release.BuildID, &release.ProjectID, &jobID, &release.ApplicationVersion, &status, &autoChannel, &reason, &release.CreatedAt, &release.CompletedAt, &release.FailedAt, &release.UpdatedAt)
	release.Status, release.AutoChannelName, release.FailureReason = domain.ApplicationReleaseStatus(status), autoChannel.String, reason.String
	if jobID.Valid {
		value := jobID.String
		release.JobID = &value
	}
	return release, err
}

func scanApplicationReleaseOutput(row interface{ Scan(...any) error }) (domain.ApplicationReleaseOutput, error) {
	var output domain.ApplicationReleaseOutput
	var kind, status string
	var artifactID, versionID, jobID, reference, reason sql.NullString
	err := row.Scan(&output.ID, &kind, &output.OutputKey, &output.DisplayName, &output.DefinitionJSON, &status, &artifactID, &versionID, &jobID, &reference, &reason, &output.PublishedAt, &output.FailedAt, &output.CreatedAt, &output.UpdatedAt)
	output.Kind, output.Status, output.ImmutableReference, output.FailureReason = domain.ApplicationReleaseOutputKind(kind), domain.ApplicationReleaseOutputStatus(status), reference.String, reason.String
	if artifactID.Valid {
		value := artifactID.String
		output.ArtifactID = &value
	}
	if versionID.Valid {
		value := versionID.String
		output.ArtifactVersionID = &value
	}
	if jobID.Valid {
		value := jobID.String
		output.ExecutionJobID = &value
	}
	return output, err
}

func nullableSQLString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
func applicationReleaseUniqueViolation(err error) bool {
	return strings.Contains(fmt.Sprint(err), "duplicate key")
}

var _ repository.ApplicationReleaseRepository = (*ApplicationReleaseRepository)(nil)
