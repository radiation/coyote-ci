package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	releasesvc "github.com/radiation/coyote-ci/backend/internal/service/release"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
)

func TestApplicationReleaseRepository_FinalizationAndPromotionTransactions_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	repo := NewApplicationReleaseRepository(db)

	t.Run("finalization moves latest once and is repeatable", func(t *testing.T) {
		release := createApplicationReleaseIntegrationFixture(t, db, "latest")
		reconciled, reconcileErr := repo.Reconcile(ctx, release.ID)
		if reconcileErr != nil {
			t.Fatalf("reconcile release: %v", reconcileErr)
		}
		if reconciled.Status != domain.ApplicationReleaseStatusComplete {
			t.Fatalf("release status=%q, want complete", reconciled.Status)
		}
		assertApplicationChannelState(t, db, release, "latest", 1)

		repeated, repeatedErr := repo.Reconcile(ctx, release.ID)
		if repeatedErr != nil {
			t.Fatalf("repeat reconcile release: %v", repeatedErr)
		}
		if repeated.Status != domain.ApplicationReleaseStatusComplete {
			t.Fatalf("repeated release status=%q, want complete", repeated.Status)
		}
		assertApplicationChannelState(t, db, release, "latest", 1)
	})

	t.Run("finalization rollback leaves no channel movement", func(t *testing.T) {
		release := createApplicationReleaseIntegrationFixture(t, db, "latest")
		addReleaseImageFixture(t, db, release, 1)
		functionName := "fail_application_release_event_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
		if _, createErr := db.ExecContext(ctx, fmt.Sprintf(`
			CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION 'injected channel event failure'; END;
			$$
		`, functionName)); createErr != nil {
			t.Fatalf("create failure function: %v", createErr)
		}
		if _, createErr := db.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON application_channel_events FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, functionName)); createErr != nil {
			t.Fatalf("create failure trigger: %v", createErr)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON application_channel_events", functionName))
			_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
		})

		if _, reconcileErr := repo.Reconcile(ctx, release.ID); reconcileErr == nil {
			t.Fatal("expected injected finalization failure")
		}
		var status string
		if statusErr := db.QueryRowContext(ctx, `SELECT status FROM application_releases WHERE id=$1`, release.ID).Scan(&status); statusErr != nil {
			t.Fatalf("load release status: %v", statusErr)
		}
		if status != string(domain.ApplicationReleaseStatusPending) {
			t.Fatalf("release status=%q, want pending", status)
		}
		assertApplicationChannelState(t, db, release, "latest", 0)
		pending, getErr := repo.GetByID(ctx, release.ID)
		if getErr != nil || len(pending.Outputs) != 1 || pending.Outputs[0].Status != domain.ApplicationReleaseOutputPending || pending.Outputs[0].ExecutionJobID != nil {
			t.Fatalf("image snapshot escaped rollback: %+v %v", pending, getErr)
		}
		if _, dropErr := db.ExecContext(ctx, fmt.Sprintf("DROP TRIGGER %s ON application_channel_events", functionName)); dropErr != nil {
			t.Fatal(dropErr)
		}
		retried, retryErr := repo.Reconcile(ctx, release.ID)
		if retryErr != nil || retried.Status != domain.ApplicationReleaseStatusComplete {
			t.Fatalf("finalization retry failed: %+v %v", retried, retryErr)
		}
		assertApplicationChannelState(t, db, release, "latest", 1)
	})

	t.Run("concurrent finalization and promotion produce one event", func(t *testing.T) {
		release := createApplicationReleaseIntegrationFixture(t, db, "latest")
		runConcurrentReleaseOperation(t, 2, func() error {
			_, reconcileErr := repo.Reconcile(context.Background(), release.ID)
			return reconcileErr
		})
		assertApplicationChannelState(t, db, release, "latest", 1)

		runConcurrentReleaseOperation(t, 3, func() error {
			_, promoteErr := repo.PromoteStable(context.Background(), release.ID, nil)
			return promoteErr
		})
		assertApplicationChannelState(t, db, release, "stable", 1)
	})
}

func createApplicationReleaseIntegrationFixture(t *testing.T, db *sql.DB, autoChannel string) domain.ApplicationRelease {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	projectID, jobID, buildID, releaseID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, projectErr := db.ExecContext(ctx, `INSERT INTO projects (id, name, slug, description, created_at, updated_at) VALUES ($1,$2,$3,NULL,$4,$4)`, projectID, "release project "+projectID, "release-project-"+uuid.NewString(), now); projectErr != nil {
		t.Fatalf("insert project: %v", projectErr)
	}
	if _, jobErr := db.ExecContext(ctx, `INSERT INTO jobs (id, project_id, name, priority, repository_url, push_enabled, trigger_mode, branch_allowlist, tag_allowlist, enabled, created_at, updated_at) VALUES ($1,$2,$3,5,'https://example.invalid/repo.git',FALSE,'branches','[]'::jsonb,'[]'::jsonb,TRUE,$4,$4)`, jobID, projectID, "release-job-"+jobID, now); jobErr != nil {
		t.Fatalf("insert job: %v", jobErr)
	}
	if _, buildErr := db.ExecContext(ctx, `INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind) VALUES ($1,1,$2,$3,5,'success',$4,0,1,'manual','external')`, buildID, projectID, jobID, now); buildErr != nil {
		t.Fatalf("insert build: %v", buildErr)
	}
	if _, releaseErr := db.ExecContext(ctx, `INSERT INTO application_releases (id, build_id, project_id, job_id, application_version, status, auto_channel_name, created_at, updated_at) VALUES ($1,$2,$3,$4,'1.2.3','pending',$5,$6,$6)`, releaseID, buildID, projectID, jobID, autoChannel, now); releaseErr != nil {
		t.Fatalf("insert application release: %v", releaseErr)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM application_channel_events WHERE channel_id IN (SELECT id FROM application_channels WHERE project_id=$1)`, projectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM application_channels WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM application_releases WHERE id=$1`, releaseID)
		_, _ = db.ExecContext(ctx, `DELETE FROM builds WHERE id=$1`, buildID)
		_, _ = db.ExecContext(ctx, `DELETE FROM jobs WHERE id=$1`, jobID)
		_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id=$1`, projectID)
	})
	return domain.ApplicationRelease{ID: releaseID, BuildID: buildID, ProjectID: projectID, JobID: &jobID, ApplicationVersion: "1.2.3"}
}

func TestApplicationReleaseRepository_Outputs_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)
	ctx := context.Background()
	repo := NewApplicationReleaseRepository(db)
	for _, imageRows := range []int{1, 2} {
		t.Run(fmt.Sprintf("image cursor with %d rows", imageRows), func(t *testing.T) {
			release := createApplicationReleaseIntegrationFixture(t, db, "")
			addReleaseImageFixture(t, db, release, imageRows)
			reconciled, reconcileErr := repo.Reconcile(ctx, release.ID)
			if reconcileErr != nil {
				t.Fatalf("real pgx image reconciliation: %v", reconcileErr)
			}
			if reconciled.Status != domain.ApplicationReleaseStatusComplete || len(reconciled.Outputs) != 1 || reconciled.Outputs[0].ExecutionJobID == nil {
				t.Fatalf("unverified image release: %+v", reconciled)
			}
		})
	}
	for _, stepScoped := range []bool{false, true} {
		t.Run(fmt.Sprintf("step scoped %t missing version recovery", stepScoped), func(t *testing.T) {
			release := createApplicationReleaseIntegrationFixture(t, db, "")
			addReleaseImageFixture(t, db, release, 2)
			artifact := addReleaseArtifactFixture(t, db, release, stepScoped, "bundle", "dist/bundle.tgz")
			addReleaseArtifactOutput(t, db, release, "bundle", "dist/bundle.tgz")
			pending, pendingErr := repo.Reconcile(ctx, release.ID)
			if pendingErr != nil || pending.Status != domain.ApplicationReleaseStatusPending {
				t.Fatalf("missing version must stay pending: %+v, %v", pending, pendingErr)
			}
			for _, output := range pending.Outputs {
				if output.Kind == domain.ApplicationReleaseOutputArtifact && (output.Status != domain.ApplicationReleaseOutputPending || output.ArtifactVersionID != nil) {
					t.Fatalf("artifact published without version: %+v", output)
				}
			}
			labels := NewArtifactLabelRepository(db)
			params := repository.CreateArtifactLabelsParams{JobID: *release.JobID, Value: release.ApplicationVersion, Kind: domain.VersionTagKindVersion, ArtifactIDs: []string{artifact.ID}}
			runConcurrentReleaseOperation(t, 4, func() error {
				_, createErr := labels.CreateForArtifacts(ctx, params)
				return createErr
			})
			runConcurrentReleaseOperation(t, 4, func() error {
				_, reconcileErr := repo.Reconcile(ctx, release.ID)
				return reconcileErr
			})
			complete, completeErr := repo.Reconcile(ctx, release.ID)
			if completeErr != nil || complete.Status != domain.ApplicationReleaseStatusComplete {
				t.Fatalf("version recovery failed: %+v, %v", complete, completeErr)
			}
			for _, output := range complete.Outputs {
				if output.Kind == domain.ApplicationReleaseOutputArtifact && (output.ArtifactID == nil || *output.ArtifactID != artifact.ID || output.ArtifactVersionID == nil || output.ImmutableReference != "1.2.3:"+artifact.ID) {
					t.Fatalf("wrong artifact identity: %+v", output)
				}
			}
			assertReleaseNoChannels(t, db, release)
		})
	}
	t.Run("ambiguous image publications conflict", func(t *testing.T) {
		release := createApplicationReleaseIntegrationFixture(t, db, "")
		addReleaseImageFixture(t, db, release, 2)
		if _, updateErr := db.ExecContext(ctx, `UPDATE external_image_builds SET published_image_reference='registry.example/acme/server:1.2.3' WHERE execution_job_id IN (SELECT id FROM build_jobs WHERE build_id=$1)`, release.BuildID); updateErr != nil {
			t.Fatal(updateErr)
		}
		if _, reconcileErr := repo.Reconcile(ctx, release.ID); !errors.Is(reconcileErr, repository.ErrApplicationReleaseConflict) {
			t.Fatalf("ambiguous image matched: %v", reconcileErr)
		}
		pending, getErr := repo.GetByID(ctx, release.ID)
		if getErr != nil || pending.Status != domain.ApplicationReleaseStatusPending || pending.Outputs[0].Status != domain.ApplicationReleaseOutputPending {
			t.Fatalf("ambiguous image published: %+v %v", pending, getErr)
		}
		assertReleaseNoChannels(t, db, release)
	})
	t.Run("ambiguous artifacts roll back image update and retry", func(t *testing.T) {
		release := createApplicationReleaseIntegrationFixture(t, db, "")
		addReleaseImageFixture(t, db, release, 1)
		first := addReleaseArtifactFixture(t, db, release, false, "bundle", "dist/bundle.tgz")
		second := addReleaseArtifactFixture(t, db, release, true, "bundle", "dist/bundle.tgz")
		addReleaseArtifactOutput(t, db, release, "bundle", "dist/bundle.tgz")
		if _, createErr := NewArtifactLabelRepository(db).CreateForArtifacts(ctx, repository.CreateArtifactLabelsParams{JobID: *release.JobID, Value: "1.2.3", Kind: domain.VersionTagKindVersion, ArtifactIDs: []string{first.ID}}); createErr != nil {
			t.Fatal(createErr)
		}
		if _, reconcileErr := repo.Reconcile(ctx, release.ID); !errors.Is(reconcileErr, repository.ErrApplicationReleaseConflict) {
			t.Fatalf("ambiguous artifacts should conflict: %v", reconcileErr)
		}
		pending, getErr := repo.GetByID(ctx, release.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		for _, output := range pending.Outputs {
			if output.Status != domain.ApplicationReleaseOutputPending {
				t.Fatalf("partial update escaped rollback: %+v", output)
			}
		}
		if _, deleteErr := db.ExecContext(ctx, `DELETE FROM build_artifacts WHERE id=$1`, second.ID); deleteErr != nil {
			t.Fatal(deleteErr)
		}
		complete, retryErr := repo.Reconcile(ctx, release.ID)
		if retryErr != nil || complete.Status != domain.ApplicationReleaseStatusComplete {
			t.Fatalf("retry after rollback: %+v %v", complete, retryErr)
		}
		assertReleaseNoChannels(t, db, release)
	})
	for _, mismatch := range []string{"name", "path", "version", "build", "package", "step"} {
		t.Run("reject unrelated "+mismatch, func(t *testing.T) {
			release := createApplicationReleaseIntegrationFixture(t, db, "")
			artifact := addReleaseArtifactFixture(t, db, release, true, "bundle", "dist/bundle.tgz")
			addReleaseArtifactOutput(t, db, release, "bundle", "dist/bundle.tgz")
			if _, createErr := NewArtifactLabelRepository(db).CreateForArtifacts(ctx, repository.CreateArtifactLabelsParams{JobID: *release.JobID, Value: "1.2.3", Kind: domain.VersionTagKindVersion, ArtifactIDs: []string{artifact.ID}}); createErr != nil {
				t.Fatal(createErr)
			}
			var query string
			var value any = "different"
			switch mismatch {
			case "name":
				query = `UPDATE build_artifacts SET artifact_name=$2 WHERE id=$1`
			case "path":
				query = `UPDATE build_artifacts SET logical_path=$2 WHERE id=$1`
			case "version":
				query = `UPDATE artifact_versions SET version_text=$2 WHERE artifact_id=$1`
			case "build", "step":
				other := createApplicationReleaseIntegrationFixture(t, db, "")
				value = other.BuildID
				query = `UPDATE build_artifacts SET build_id=$2 WHERE id=$1`
				if mismatch == "step" {
					query = `UPDATE build_steps SET build_id=$2 WHERE id=(SELECT step_id FROM build_artifacts WHERE id=$1)`
				}
			case "package":
				other := addReleaseArtifactFixture(t, db, release, false, "other", "dist/other.tgz")
				value = other.PackageID
				query = `UPDATE artifact_versions SET package_id=$2 WHERE artifact_id=$1`
			}
			if _, updateErr := db.ExecContext(ctx, query, artifact.ID, value); updateErr != nil {
				t.Fatal(updateErr)
			}
			pending, reconcileErr := repo.Reconcile(ctx, release.ID)
			if reconcileErr != nil || pending.Status != domain.ApplicationReleaseStatusPending {
				t.Fatalf("unrelated artifact matched: %+v %v", pending, reconcileErr)
			}
			assertReleaseNoChannels(t, db, release)
		})
	}
}

func addReleaseArtifactFixture(t *testing.T, db *sql.DB, release domain.ApplicationRelease, stepScoped bool, name, path string) domain.BuildArtifact {
	t.Helper()
	var stepID *string
	if stepScoped {
		id := uuid.NewString()
		if _, insertErr := db.ExecContext(context.Background(), `INSERT INTO build_steps (id, build_id, step_index, name, command, status) VALUES ($1,$2,100,'artifact','true','success')`, id, release.BuildID); insertErr != nil {
			t.Fatal(insertErr)
		}
		stepID = &id
	}
	artifact, createErr := NewArtifactRepository(db).Create(context.Background(), domain.BuildArtifact{ID: uuid.NewString(), BuildID: release.BuildID, StepID: stepID, Name: name, LogicalPath: path, StorageKey: "test/" + uuid.NewString(), SizeBytes: 12, CreatedAt: time.Now().UTC()})
	if createErr != nil {
		t.Fatal(createErr)
	}
	return artifact
}

func TestArtifactLabelRepository_ImmutableVersions_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	t.Cleanup(func() { closeNotificationIntegrationDB(t, db) })
	ctx := context.Background()
	release := createApplicationReleaseIntegrationFixture(t, db, "")
	first := addReleaseArtifactFixture(t, db, release, false, "bundle", "dist/bundle.tgz")
	second := addReleaseArtifactFixture(t, db, release, true, "bundle", "dist/bundle.tgz")
	extra := addReleaseArtifactFixture(t, db, release, false, "extra", "dist/extra.tgz")
	labels := NewArtifactLabelRepository(db)
	create := func(ids ...string) ([]domain.VersionTag, error) {
		return labels.CreateForArtifacts(ctx, repository.CreateArtifactLabelsParams{JobID: *release.JobID, Value: "1.2.3", Kind: domain.VersionTagKindVersion, ArtifactIDs: ids})
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, artifactID := range []string{first.ID, second.ID} {
		go func() {
			<-start
			_, createErr := create(artifactID)
			results <- createErr
		}()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		createErr := <-results
		switch {
		case createErr == nil:
			successes++
		case errors.Is(createErr, repository.ErrVersionTagConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent insertion error: %v", createErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	var versionID, ownerID string
	if queryErr := db.QueryRowContext(ctx, `SELECT id, artifact_id FROM artifact_versions WHERE package_id=$1 AND version_text='1.2.3'`, first.PackageID).Scan(&versionID, &ownerID); queryErr != nil {
		t.Fatal(queryErr)
	}
	loserID := first.ID
	if loserID == ownerID {
		loserID = second.ID
	}
	for range 2 {
		if _, conflictErr := create(extra.ID, loserID); !errors.Is(conflictErr, repository.ErrVersionTagConflict) {
			t.Fatalf("batch conflict: %v", conflictErr)
		}
		var extraVersions int
		if queryErr := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifact_versions WHERE artifact_id=$1`, extra.ID).Scan(&extraVersions); queryErr != nil || extraVersions != 0 {
			t.Fatalf("batch did not roll back: count=%d err=%v", extraVersions, queryErr)
		}
		tags, retryErr := create(ownerID)
		if retryErr != nil || len(tags) != 1 || tags[0].ID != versionID || *tags[0].ArtifactID != ownerID {
			t.Fatalf("owner retry changed immutable identity: %+v %v", tags, retryErr)
		}
	}
	runConcurrentReleaseOperation(t, 4, func() error {
		_, retryErr := create(extra.ID, ownerID)
		return retryErr
	})
	assertReleaseNoChannels(t, db, release)
}

func TestApplicationReleaseRepository_KubernetesVersioning_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)
	ctx := context.Background()
	for _, scenario := range []string{"terminal completion", "transient version failure", "missed terminal hook", "duplicate preserves newer channel"} {
		t.Run(scenario, func(t *testing.T) {
			release := createApplicationReleaseIntegrationFixture(t, db, "")
			artifact := addReleaseArtifactFixture(t, db, release, true, "bundle", "dist/bundle.tgz")
			addReleaseArtifactOutput(t, db, release, "bundle", "dist/bundle.tgz")
			pipelineYAML := "version: 1\nsteps:\n  - name: package\n    run: true\n    artifacts:\n      - name: bundle\n        path: dist/bundle.tgz\nartifacts:\n  - name: bundle\n    path: dist/bundle.tgz\n"
			if scenario == "missed terminal hook" || scenario == "duplicate preserves newer channel" {
				pipelineYAML += "    version:\n      template: '1.2.3'\n      channel: latest\n"
			}
			if _, updateErr := db.ExecContext(ctx, `UPDATE builds SET status='running', application_version='1.2.3', pipeline_config_yaml=$2 WHERE id=$1`, release.BuildID, pipelineYAML); updateErr != nil {
				t.Fatal(updateErr)
			}
			claimToken := uuid.NewString()
			if _, updateErr := db.ExecContext(ctx, `UPDATE build_steps SET status='running', step_index=0, claim_token=$2, lease_expires_at=NOW()+INTERVAL '1 hour' WHERE id=$1`, *artifact.StepID, claimToken); updateErr != nil {
				t.Fatal(updateErr)
			}
			jobID := uuid.NewString()
			if _, insertErr := db.ExecContext(ctx, `INSERT INTO build_jobs (id, build_id, step_id, name, step_index, status, image, working_dir, command_json, source_commit_sha, resolved_spec_json, created_at, claim_token, claimed_by, claim_expires_at) VALUES ($1,$2,$3,'package',0,'running','builder','.','[]','abc','{}',NOW(),$4,'worker',NOW()+INTERVAL '1 hour')`, jobID, release.BuildID, *artifact.StepID, claimToken); insertErr != nil {
				t.Fatal(insertErr)
			}
			buildService := newReleaseIntegrationBuildService(db)
			workerService := workersvc.NewExecutionWorkerService(buildService)
			step := workersvc.WorkerRunnableStep{BuildID: release.BuildID, StepID: *artifact.StepID, StepIndex: 0, StepName: "package", JobID: jobID, ClaimToken: claimToken, WorkerID: "worker"}
			result := runner.RunStepResult{Status: runner.RunStepStatusSuccess, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}
			removeFailure := func() {}
			if scenario == "transient version failure" {
				removeFailure = failArtifactVersionInserts(t, db, artifact.ID)
			}
			if scenario == "missed terminal hook" {
				buildService = buildsvc.NewBuildServiceFromConfig(NewBuildRepository(db), nil, nil, buildsvc.BuildServiceConfig{ExecutionJobRepo: NewExecutionJobRepository(db)})
				workerService = workersvc.NewExecutionWorkerService(buildService)
			}
			outcome, completeErr := workerService.CompleteKubernetesRunnableStep(ctx, step, result)
			if outcome != repository.StepCompletionCompleted {
				t.Fatalf("completion outcome=%s err=%v", outcome, completeErr)
			}
			if (completeErr != nil) != (scenario == "transient version failure") {
				t.Fatalf("unexpected completion error: %v", completeErr)
			}
			persistedBuild, buildErr := NewBuildRepository(db).GetByID(ctx, release.BuildID)
			if buildErr != nil || persistedBuild.Status != domain.BuildStatusSuccess {
				t.Fatalf("build completion not durable: %+v %v", persistedBuild, buildErr)
			}
			labels, labelErr := NewArtifactLabelRepository(db).ListByArtifactID(ctx, artifact.ID)
			wantLabels := 0
			if scenario == "terminal completion" {
				wantLabels = 1
			}
			if scenario == "duplicate preserves newer channel" {
				wantLabels = 2
			}
			if labelErr != nil || len(labels) != wantLabels {
				t.Fatalf("terminal artifact versions: %+v %v", labels, labelErr)
			}
			recovery := releasesvc.NewService(NewApplicationReleaseRepository(db)).WithArtifactVersioner(newReleaseIntegrationBuildService(db))
			if scenario == "transient version failure" {
				if _, recoveryErr := recovery.Reconcile(ctx, release.ID); recoveryErr == nil {
					t.Fatal("version failure must prevent reconciliation")
				}
				pending, pendingErr := NewApplicationReleaseRepository(db).GetByID(ctx, release.ID)
				if pendingErr != nil || pending.Status != domain.ApplicationReleaseStatusPending {
					t.Fatalf("failed version write published release: %+v %v", pending, pendingErr)
				}
			}
			removeFailure()
			runConcurrentReleaseOperation(t, 3, func() error { return recovery.ReconcilePending(ctx, 100) })
			complete, getErr := recovery.Get(ctx, release.ID)
			if getErr != nil || complete.Status != domain.ApplicationReleaseStatusComplete {
				t.Fatalf("durable recovery failed: %+v %v", complete, getErr)
			}
			var newerArtifactID string
			if scenario == "duplicate preserves newer channel" {
				newerBuildID := uuid.NewString()
				if _, insertErr := db.ExecContext(ctx, `INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind) VALUES ($1,2,$2,$3,5,'success',NOW(),0,1,'manual','external')`, newerBuildID, release.ProjectID, *release.JobID); insertErr != nil {
					t.Fatal(insertErr)
				}
				t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM builds WHERE id=$1`, newerBuildID) })
				newerRelease := release
				newerRelease.BuildID = newerBuildID
				newer := addReleaseArtifactFixture(t, db, newerRelease, false, "bundle", "dist/bundle.tgz")
				newerArtifactID = newer.ID
				if _, channelErr := NewArtifactLabelRepository(db).CreateForArtifacts(ctx, repository.CreateArtifactLabelsParams{JobID: *release.JobID, Value: "latest", Kind: domain.VersionTagKindChannel, ArtifactIDs: []string{newer.ID}}); channelErr != nil {
					t.Fatal(channelErr)
				}
			}
			workerService = workersvc.NewExecutionWorkerService(newReleaseIntegrationBuildService(db))
			runConcurrentReleaseOperation(t, 3, func() error {
				duplicate, duplicateErr := workerService.CompleteKubernetesRunnableStep(ctx, step, result)
				if duplicate != repository.StepCompletionDuplicateTerminal {
					return fmt.Errorf("duplicate completion outcome=%s", duplicate)
				}
				return duplicateErr
			})
			labels, labelErr = NewArtifactLabelRepository(db).ListByArtifactID(ctx, artifact.ID)
			if labelErr != nil || len(labels) != 1 || labels[0].Version != "1.2.3" {
				t.Fatalf("retry labels: %+v %v", labels, labelErr)
			}
			if newerArtifactID == "" {
				assertReleaseNoChannels(t, db, release)
			} else {
				var currentArtifactID string
				var events int
				if queryErr := db.QueryRowContext(ctx, `SELECT c.current_artifact_id, (SELECT COUNT(*) FROM artifact_channel_events e WHERE e.package_id=c.package_id) FROM artifact_channels c WHERE c.package_id=$1 AND c.channel_name='latest'`, artifact.PackageID).Scan(&currentArtifactID, &events); queryErr != nil || currentArtifactID != newerArtifactID || events != 2 {
					t.Fatalf("duplicate moved channel: current=%s events=%d err=%v", currentArtifactID, events, queryErr)
				}
			}
		})
	}
}

func newReleaseIntegrationBuildService(db *sql.DB) *buildsvc.BuildService {
	return buildsvc.NewBuildServiceFromConfig(NewBuildRepository(db), nil, nil, buildsvc.BuildServiceConfig{
		ExecutionJobRepo: NewExecutionJobRepository(db),
		ArtifactRepo:     NewArtifactRepository(db),
		VersionTagger:    versiontagsvc.NewService(nil).WithArtifactLabels(NewArtifactLabelRepository(db)),
	})
}

type pausedTerminalBuildLookup struct {
	repository.BuildRepository
	reached chan struct{}
	resume  chan struct{}
}

func (r *pausedTerminalBuildLookup) GetByID(ctx context.Context, buildID string) (domain.Build, error) {
	close(r.reached)
	select {
	case <-r.resume:
		return r.BuildRepository.GetByID(ctx, buildID)
	case <-ctx.Done():
		return domain.Build{}, ctx.Err()
	}
}

func TestApplicationReleaseRepository_DelayedKubernetesCompletionDoesNotReplayChannel_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	t.Cleanup(func() { closeNotificationIntegrationDB(t, db) })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	release := createApplicationReleaseIntegrationFixture(t, db, "")
	artifact := addReleaseArtifactFixture(t, db, release, true, "bundle", "dist/bundle.tgz")
	pipelineYAML := "version: 1\nsteps:\n  - name: package\n    run: true\n  - name: finish\n    run: true\nartifacts:\n  - name: bundle\n    path: dist/bundle.tgz\n    version:\n      template: '1.2.3'\n      channel: latest\n"
	if _, updateErr := db.ExecContext(ctx, `UPDATE builds SET status='running', application_version='1.2.3', pipeline_config_yaml=$2 WHERE id=$1`, release.BuildID, pipelineYAML); updateErr != nil {
		t.Fatal(updateErr)
	}
	steps := make([]workersvc.WorkerRunnableStep, 2)
	for index := range steps {
		stepID, jobID, token := uuid.NewString(), uuid.NewString(), uuid.NewString()
		if index == 0 {
			stepID = *artifact.StepID
			if _, updateErr := db.ExecContext(ctx, `UPDATE build_steps SET status='running', step_index=0, claim_token=$2 WHERE id=$1`, stepID, token); updateErr != nil {
				t.Fatal(updateErr)
			}
		} else if _, insertErr := db.ExecContext(ctx, `INSERT INTO build_steps (id, build_id, step_index, name, command, status, claim_token) VALUES ($1,$2,1,'finish','true','running',$3)`, stepID, release.BuildID, token); insertErr != nil {
			t.Fatal(insertErr)
		}
		if _, insertErr := db.ExecContext(ctx, `INSERT INTO build_jobs (id, build_id, step_id, name, step_index, status, image, working_dir, command_json, source_commit_sha, resolved_spec_json, created_at, claim_token, claimed_by, claim_expires_at) VALUES ($1,$2,$3,'step',$4,'running','builder','.','[]','abc','{}',NOW(),$5,'worker',NOW()+INTERVAL '1 hour')`, jobID, release.BuildID, stepID, index, token); insertErr != nil {
			t.Fatal(insertErr)
		}
		steps[index] = workersvc.WorkerRunnableStep{BuildID: release.BuildID, StepID: stepID, StepIndex: index, StepName: "step", JobID: jobID, ClaimToken: token}
	}
	paused := &pausedTerminalBuildLookup{BuildRepository: NewBuildRepository(db), reached: make(chan struct{}), resume: make(chan struct{})}
	defer close(paused.resume)
	firstService := buildsvc.NewBuildServiceFromConfig(paused, nil, nil, buildsvc.BuildServiceConfig{
		ExecutionJobRepo: NewExecutionJobRepository(db), ArtifactRepo: NewArtifactRepository(db),
		VersionTagger: versiontagsvc.NewService(nil).WithArtifactLabels(NewArtifactLabelRepository(db)),
	})
	firstWorker := workersvc.NewExecutionWorkerService(firstService)
	worker := workersvc.NewExecutionWorkerService(newReleaseIntegrationBuildService(db))
	result := runner.RunStepResult{Status: runner.RunStepStatusSuccess, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}
	delayed := make(chan error, 1)
	go func() {
		_, completeErr := firstWorker.CompleteKubernetesRunnableStep(ctx, steps[0], result)
		delayed <- completeErr
	}()
	select {
	case <-paused.reached:
	case <-ctx.Done():
		t.Fatal("first completion never reached post-terminal lookup")
	}
	var completedReports atomic.Int32
	runConcurrentReleaseOperation(t, 3, func() error {
		outcome, completeErr := worker.CompleteKubernetesRunnableStep(ctx, steps[1], result)
		if outcome == repository.StepCompletionCompleted {
			completedReports.Add(1)
		} else if outcome != repository.StepCompletionDuplicateTerminal {
			return fmt.Errorf("unexpected final completion outcome: %s", outcome)
		}
		return completeErr
	})
	if completedReports.Load() != 1 {
		t.Fatalf("final step completed %d times", completedReports.Load())
	}
	newerBuildID := uuid.NewString()
	if _, insertErr := db.ExecContext(ctx, `INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind) VALUES ($1,2,$2,$3,5,'success',NOW(),0,1,'manual','external')`, newerBuildID, release.ProjectID, *release.JobID); insertErr != nil {
		t.Fatal(insertErr)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), `DELETE FROM builds WHERE id=$1`, newerBuildID) })
	newerRelease := release
	newerRelease.BuildID = newerBuildID
	newer := addReleaseArtifactFixture(t, db, newerRelease, false, "bundle", "dist/bundle.tgz")
	if _, channelErr := NewArtifactLabelRepository(db).CreateForArtifacts(ctx, repository.CreateArtifactLabelsParams{JobID: *release.JobID, Value: "latest", Kind: domain.VersionTagKindChannel, ArtifactIDs: []string{newer.ID}}); channelErr != nil {
		t.Fatal(channelErr)
	}
	select {
	case paused.resume <- struct{}{}:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if completeErr := <-delayed; completeErr != nil {
		t.Fatal(completeErr)
	}
	for _, step := range steps {
		if outcome, duplicateErr := worker.CompleteKubernetesRunnableStep(ctx, step, result); duplicateErr != nil || outcome != repository.StepCompletionDuplicateTerminal {
			t.Fatalf("repeated completion: %s %v", outcome, duplicateErr)
		}
		if recoveryErr := newReleaseIntegrationBuildService(db).ReconcileArtifactVersions(ctx, release.BuildID); recoveryErr != nil {
			t.Fatal(recoveryErr)
		}
	}
	var currentArtifactID string
	var events int
	if queryErr := db.QueryRowContext(ctx, `SELECT c.current_artifact_id, (SELECT COUNT(*) FROM artifact_channel_events e WHERE e.package_id=c.package_id) FROM artifact_channels c WHERE c.package_id=$1 AND c.channel_name='latest'`, artifact.PackageID).Scan(&currentArtifactID, &events); queryErr != nil || currentArtifactID != newer.ID || events != 2 {
		t.Fatalf("delayed completion replayed channel: current=%s want=%s events=%d err=%v", currentArtifactID, newer.ID, events, queryErr)
	}
}

func failArtifactVersionInserts(t *testing.T, db *sql.DB, artifactID string) func() {
	t.Helper()
	name := "fail_artifact_version_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	if _, createErr := db.ExecContext(context.Background(), fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected version failure'; END; $$`, name)); createErr != nil {
		t.Fatal(createErr)
	}
	if _, createErr := db.ExecContext(context.Background(), fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON artifact_versions FOR EACH ROW WHEN (NEW.artifact_id='%s') EXECUTE FUNCTION %s()`, name, artifactID, name)); createErr != nil {
		t.Fatal(createErr)
	}
	remove := func() {
		if _, dropErr := db.ExecContext(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON artifact_versions; DROP FUNCTION IF EXISTS %s()`, name, name)); dropErr != nil {
			t.Fatal(dropErr)
		}
	}
	t.Cleanup(remove)
	return remove
}

func addReleaseArtifactOutput(t *testing.T, db *sql.DB, release domain.ApplicationRelease, name, path string) {
	t.Helper()
	if _, insertErr := db.ExecContext(context.Background(), `INSERT INTO application_release_outputs (id, release_id, output_kind, output_key, display_name, definition_json, status) VALUES ($1,$2,'artifact',$3,$4,jsonb_build_object('name',$4::text,'path',$5::text),'pending')`, uuid.NewString(), release.ID, "artifact:"+name, name, path); insertErr != nil {
		t.Fatal(insertErr)
	}
}

func addReleaseImageFixture(t *testing.T, db *sql.DB, release domain.ApplicationRelease, count int) {
	t.Helper()
	ctx := context.Background()
	if _, insertErr := db.ExecContext(ctx, `INSERT INTO application_release_outputs (id, release_id, output_kind, output_key, display_name, definition_json, status) VALUES ($1,$2,'image','image:server','server','{"node_id":"server","target_image_reference":"acme/server"}','pending')`, uuid.NewString(), release.ID); insertErr != nil {
		t.Fatal(insertErr)
	}
	for index := 0; index < count; index++ {
		stepID, jobID := uuid.NewString(), uuid.NewString()
		if _, insertErr := db.ExecContext(ctx, `INSERT INTO build_steps (id, build_id, step_index, node_id, name, command, status) VALUES ($1,$2,$3,'server','server','true','success')`, stepID, release.BuildID, index); insertErr != nil {
			t.Fatal(insertErr)
		}
		if _, insertErr := db.ExecContext(ctx, `INSERT INTO build_jobs (id, build_id, step_id, name, step_index, status, image, working_dir, command_json, source_commit_sha, resolved_spec_json, created_at) VALUES ($1,$2,$3,'server',$4,'success','builder','.','[]','abc','{}',NOW())`, jobID, release.BuildID, stepID, index); insertErr != nil {
			t.Fatal(insertErr)
		}
		version := release.ApplicationVersion
		if index > 0 {
			version = "unrelated"
		}
		if _, insertErr := db.ExecContext(ctx, `INSERT INTO external_image_builds (execution_job_id, provider, submission_state, target_image_reference, terminal_result, published_image_reference, published_image_digest_reference, created_at, updated_at) VALUES ($1,'cloud_build','terminal','acme/server','success',$2,$3,NOW(),NOW())`, jobID, "registry.example/acme/server:"+version, "registry.example/acme/server@sha256:"+strings.Repeat("a", 64)); insertErr != nil {
			t.Fatal(insertErr)
		}
	}
}

func assertReleaseNoChannels(t *testing.T, db *sql.DB, release domain.ApplicationRelease) {
	t.Helper()
	var count int
	if queryErr := db.QueryRowContext(context.Background(), `SELECT (SELECT COUNT(*) FROM application_channels WHERE project_id=$1) + (SELECT COUNT(*) FROM artifact_channels c JOIN artifact_packages p ON p.id=c.package_id WHERE p.project_id=$1::text) + (SELECT COUNT(*) FROM artifact_channel_events e JOIN artifact_packages p ON p.id=e.package_id WHERE p.project_id=$1::text)`, release.ProjectID).Scan(&count); queryErr != nil {
		t.Fatal(queryErr)
	}
	if count != 0 {
		t.Fatalf("unexpected channel movement: %d rows", count)
	}
}

func assertApplicationChannelState(t *testing.T, db *sql.DB, release domain.ApplicationRelease, channelName string, wantEvents int) {
	t.Helper()
	var currentReleaseID string
	var events int
	queryErr := db.QueryRowContext(context.Background(), `SELECT c.current_release_id, COUNT(e.id) FROM application_channels c LEFT JOIN application_channel_events e ON e.channel_id=c.id WHERE c.project_id=$1 AND c.job_id=$2 AND c.channel_name=$3 GROUP BY c.current_release_id`, release.ProjectID, *release.JobID, channelName).Scan(&currentReleaseID, &events)
	if wantEvents == 0 {
		if queryErr == nil {
			t.Fatalf("unexpected %s channel current=%s events=%d", channelName, currentReleaseID, events)
		}
		return
	}
	if queryErr != nil {
		t.Fatalf("load %s channel: %v", channelName, queryErr)
	}
	if currentReleaseID != release.ID || events != wantEvents {
		t.Fatalf("%s channel current=%s events=%d, want current=%s events=%d", channelName, currentReleaseID, events, release.ID, wantEvents)
	}
}

func runConcurrentReleaseOperation(t *testing.T, count int, operation func() error) {
	t.Helper()
	start := make(chan struct{})
	errors := make(chan error, count)
	var waitGroup sync.WaitGroup
	for index := 0; index < count; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			errors <- operation()
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errors)
	for operationErr := range errors {
		if operationErr != nil {
			t.Fatalf("concurrent release operation: %v", operationErr)
		}
	}
}
