package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (r *ArtifactRepository) Browse(ctx context.Context, params repository.BrowseArtifactsParams) ([]domain.ArtifactRecord, error) {
	selectColumns := `
		` + qualifyColumns("a", artifactColumns) + `,
		` + qualifyColumns("b", buildListColumns) + `,
		s.id,
		s.step_index,
		s.name
	`
	browseQuery := `
		SELECT ` + selectColumns + `
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		LEFT JOIN build_steps s ON s.id = a.step_id
		WHERE (
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
		ORDER BY a.created_at DESC, a.logical_path ASC, b.created_at DESC
	`

	trimmedQuery := strings.TrimSpace(params.Query)
	likeQuery := "%" + trimmedQuery + "%"
	args := []any{trimmedQuery, likeQuery}
	if params.Limit > 0 {
		args = append(args, params.Limit)
		browseQuery += fmt.Sprintf("\n\t\tLIMIT $%d", len(args))
	}
	if params.Offset > 0 {
		args = append(args, params.Offset)
		browseQuery += fmt.Sprintf("\n\t\tOFFSET $%d", len(args))
	}

	rows, err := r.db.QueryContext(ctx, browseQuery, args...)
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
