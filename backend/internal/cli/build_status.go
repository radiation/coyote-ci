package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

func (a *app) newBuildStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <build-id>",
		Short: "Show build status and metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			build, buildErr := client.GetBuild(cmd.Context(), args[0])
			if buildErr != nil {
				return mapCommandError(buildErr)
			}
			steps, stepsErr := client.GetBuildSteps(cmd.Context(), args[0])
			if stepsErr != nil {
				return mapCommandError(stepsErr)
			}

			payload := makeBuildStatusPayload(resolved.ServerURL, build, steps)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeBuildStatusHuman(w, payload)
			}, payload)
		},
	}
}

func makeBuildStatusPayload(serverURL string, build api.BuildResponse, steps []api.BuildStepResponse) buildStatusPayload {
	jobName := firstNonEmptyPtr(build.JobName, firstJobName(steps))
	refValue := firstNonEmptyPtr(build.SourceRef, build.TriggerRef)
	shaValue := firstNonEmptyPtr(build.SourceCommitSHA, build.SourceSHA, build.TriggerCommitSHA)
	authorValue := firstNonEmptyPtr(build.SourceAuthorName, build.TriggeredBy, build.Actor)
	failedStep := firstFailedStep(steps)
	durationMS := buildDurationMS(build.StartedAt, build.FinishedAt)
	webURL := buildWebURL(serverURL, build.ID, failedStep)
	currentSteps := makeBuildCurrentStepViews(build.CurrentSteps)

	payload := buildStatusPayload{
		Build: buildStatusView{
			ID:           build.ID,
			BuildNumber:  build.BuildNumber,
			ProjectID:    build.ProjectID,
			ProjectName:  build.ProjectName,
			JobID:        build.JobID,
			JobName:      jobName,
			Status:       build.Status,
			Ref:          refValue,
			SHA:          shaValue,
			Author:       authorValue,
			CreatedAt:    build.CreatedAt,
			StartedAt:    build.StartedAt,
			FinishedAt:   build.FinishedAt,
			DurationMS:   durationMS,
			WebURL:       webURL,
			Error:        build.ErrorMessage,
			Pipeline:     build.PipelineName,
			SCMStatus:    build.SCMStatus,
			CurrentSteps: currentSteps,
		},
		FailedStep: failedStep,
	}
	return payload
}

func writeBuildStatusHuman(w io.Writer, payload buildStatusPayload) error {
	build := payload.Build
	if _, err := fmt.Fprintf(w, "Build:   %s\n", build.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Project: %s\n", displayProjectLabel(build.ProjectName, build.ProjectID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Job:     %s\n", displayJobLabel(build.JobName, build.JobID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Status:  %s\n", build.Status); err != nil {
		return err
	}
	if build.Ref != nil {
		if _, err := fmt.Fprintf(w, "Ref:     %s\n", *build.Ref); err != nil {
			return err
		}
	}
	if build.SHA != nil {
		if _, err := fmt.Fprintf(w, "Commit:  %s\n", shortSHA(*build.SHA)); err != nil {
			return err
		}
	}
	if build.Author != nil {
		if _, err := fmt.Fprintf(w, "Author:  %s\n", *build.Author); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Created: %s\n", build.CreatedAt); err != nil {
		return err
	}
	if build.StartedAt != nil {
		if _, err := fmt.Fprintf(w, "Started: %s\n", *build.StartedAt); err != nil {
			return err
		}
	}
	if build.FinishedAt != nil {
		if _, err := fmt.Fprintf(w, "Finished:%s%s\n", strings.Repeat(" ", 1), *build.FinishedAt); err != nil {
			return err
		}
	}
	if build.DurationMS != nil {
		if _, err := fmt.Fprintf(w, "Duration:%s%s\n", strings.Repeat(" ", 1), formatDurationMS(*build.DurationMS)); err != nil {
			return err
		}
	}
	if payload.FailedStep != nil {
		failedSummary := fmt.Sprintf("step %d %s (%s)", payload.FailedStep.Index, payload.FailedStep.Name, payload.FailedStep.Status)
		if payload.FailedStep.ExitCode != nil {
			failedSummary = fmt.Sprintf("step %d %s exited %d", payload.FailedStep.Index, payload.FailedStep.Name, *payload.FailedStep.ExitCode)
		}
		if _, err := fmt.Fprintf(w, "Failed:  %s\n", failedSummary); err != nil {
			return err
		}
	}
	if build.WebURL != "" {
		if _, err := fmt.Fprintf(w, "URL:     %s\n", build.WebURL); err != nil {
			return err
		}
	}
	if build.SCMStatus != nil {
		if _, err := fmt.Fprintln(w, "\nSCM status"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  Provider:       %s\n", build.SCMStatus.Provider); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  Repository:     %s/%s\n", build.SCMStatus.RepositoryOwner, build.SCMStatus.RepositoryName); err != nil {
			return err
		}
		if build.SCMStatus.CommitSHA != nil {
			if _, err := fmt.Fprintf(w, "  Commit:         %s\n", shortSHA(*build.SCMStatus.CommitSHA)); err != nil {
				return err
			}
		}
		if build.SCMStatus.Context != nil {
			if _, err := fmt.Fprintf(w, "  Context:        %s\n", *build.SCMStatus.Context); err != nil {
				return err
			}
		}
		if !build.SCMStatus.Reportable {
			if _, err := fmt.Fprintln(w, "  Reportable:     no"); err != nil {
				return err
			}
		} else if !build.SCMStatus.Configured {
			if _, err := fmt.Fprintln(w, "  Configured:     no"); err != nil {
				return err
			}
		}
		if build.SCMStatus.DesiredState != nil {
			if _, err := fmt.Fprintf(w, "  Desired state:  %s\n", *build.SCMStatus.DesiredState); err != nil {
				return err
			}
		}
		if build.SCMStatus.LastSentState != nil {
			if _, err := fmt.Fprintf(w, "  Last sent:      %s\n", *build.SCMStatus.LastSentState); err != nil {
				return err
			}
		}
		if build.SCMStatus.DeliveryState != nil {
			label := *build.SCMStatus.DeliveryState
			if build.SCMStatus.AwaitingReassertion {
				label = label + " (awaiting reassertion)"
			}
			if _, err := fmt.Fprintf(w, "  Delivery state: %s\n", label); err != nil {
				return err
			}
		}
		if build.SCMStatus.Attempts != nil {
			if _, err := fmt.Fprintf(w, "  Attempts:       %d\n", *build.SCMStatus.Attempts); err != nil {
				return err
			}
		}
		if build.SCMStatus.NextAttemptAt != nil {
			if _, err := fmt.Fprintf(w, "  Next retry:     %s\n", *build.SCMStatus.NextAttemptAt); err != nil {
				return err
			}
		}
		if build.SCMStatus.CurrentOwnerBuildID != nil {
			ownerLine := *build.SCMStatus.CurrentOwnerBuildID
			if build.SCMStatus.CurrentOwnerAttempt != nil {
				ownerLine = fmt.Sprintf("%s (attempt %d)", ownerLine, *build.SCMStatus.CurrentOwnerAttempt)
			}
			if _, err := fmt.Fprintf(w, "  Current owner:  %s\n", ownerLine); err != nil {
				return err
			}
		}
		if build.SCMStatus.LastError != nil {
			if _, err := fmt.Fprintf(w, "  Last error:     %s\n", *build.SCMStatus.LastError); err != nil {
				return err
			}
		}
	}
	if len(build.CurrentSteps) > 0 {
		if _, err := fmt.Fprintln(w, "\nRunning:"); err != nil {
			return err
		}
		for _, step := range build.CurrentSteps {
			if _, err := fmt.Fprintf(w, "  [%d] %s\n", step.Index, step.Name); err != nil {
				return err
			}
		}
	}
	return nil
}
