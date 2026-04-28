package postgres

import (
	"context"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const artifactBrowseIdentityExpression = `COALESCE(b.job_id::text, b.id::text) || '::' || a.logical_path`

const artifactBrowseFilterClause = `(
			$1 = ''
			OR COALESCE(a.artifact_name, '') ILIKE $2
			OR a.logical_path ILIKE $2
			OR b.project_id ILIKE $2
			OR COALESCE(b.job_id::text, '') ILIKE $2
			OR EXISTS (
				SELECT 1
				FROM version_tags vt
				WHERE vt.artifact_id = a.id
				  AND vt.version_text ILIKE $2
			)
		)
		AND ($3 = '' OR a.artifact_type = $3)`

func (r *ArtifactRepository) Browse(ctx context.Context, params repository.BrowseArtifactsParams) ([]domain.ArtifactRecord, error) {
	pageKeys, err := r.listBrowseIdentityKeys(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(pageKeys) == 0 {
		return []domain.ArtifactRecord{}, nil
	}

	selectColumns := `
		` + qualifyColumns("a", artifactColumns) + `,
		` + qualifyColumns("b", buildListColumns) + `,
		s.id,
		s.step_index,
		s.name
	`
	query, identityArgs := stringListQuery(`
		SELECT `+selectColumns+`
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		LEFT JOIN build_steps s ON s.id = a.step_id
		WHERE `+artifactBrowseIdentityExpression+` IN (%s)
		  AND `+artifactBrowseFilterClause+`
		ORDER BY a.created_at DESC, a.logical_path ASC, b.created_at DESC
	`, 4, pageKeys)

	trimmedQuery := strings.TrimSpace(params.Query)
	likeQuery := "%" + trimmedQuery + "%"
	args := append([]any{trimmedQuery, likeQuery, string(params.Type)}, identityArgs...)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	records := make([]domain.ArtifactRecord, 0)
	for rows.Next() {
		record, scanErr := scanArtifactBrowseRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

func (r *ArtifactRepository) listBrowseIdentityKeys(ctx context.Context, params repository.BrowseArtifactsParams) ([]string, error) {
	query := `
		SELECT page.identity_key
		FROM (
			SELECT ` + artifactBrowseIdentityExpression + ` AS identity_key,
			       a.logical_path,
			       MAX(a.created_at) AS latest_created_at
			FROM build_artifacts a
			JOIN builds b ON b.id = a.build_id
			WHERE ` + artifactBrowseFilterClause + `
			GROUP BY identity_key, a.logical_path
		) page
		ORDER BY page.latest_created_at DESC, page.logical_path ASC, page.identity_key ASC
	`

	trimmedQuery := strings.TrimSpace(params.Query)
	likeQuery := "%" + trimmedQuery + "%"
	args := []any{trimmedQuery, likeQuery, string(params.Type)}
	if params.Limit > 0 {
		query += "\n\t\tLIMIT $4"
		args = append(args, params.Limit)
		if params.Offset > 0 {
			query += "\n\t\tOFFSET $5"
			args = append(args, params.Offset)
		}
	} else if params.Offset > 0 {
		query += "\n\t\tOFFSET $4"
		args = append(args, params.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if scanErr := rows.Scan(&key); scanErr != nil {
			return nil, scanErr
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}
