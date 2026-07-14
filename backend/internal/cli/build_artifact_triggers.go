package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

func (a *app) newBuildArtifactTriggersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "artifact-triggers <build-id>",
		Short: "Inspect artifact-trigger deliveries for a build",
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

			response, responseErr := client.ListBuildArtifactTriggers(cmd.Context(), args[0])
			if responseErr != nil {
				return mapCommandError(responseErr)
			}

			payload := makeBuildArtifactTriggerDeliveriesPayload(args[0], response)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeBuildArtifactTriggersHuman(w, payload)
			}, payload)
		},
	}
}

func makeBuildArtifactTriggerDeliveriesPayload(buildID string, response api.BuildArtifactTriggerDeliveriesResponse) buildArtifactTriggerDeliveriesPayload {
	deliveries := make([]buildArtifactTriggerDeliveryView, 0, len(response.Deliveries))
	for _, delivery := range response.Deliveries {
		deliveries = append(deliveries, buildArtifactTriggerDeliveryView{
			DeliveryID:        delivery.DeliveryID,
			Status:            delivery.Status,
			CreatedAt:         delivery.CreatedAt,
			UpdatedAt:         delivery.UpdatedAt,
			ProducerBuildID:   delivery.ProducerBuildID,
			ProducerProjectID: delivery.ProducerProjectID,
			ProducerJobID:     delivery.ProducerJobID,
			ArtifactID:        delivery.ArtifactID,
			ArtifactPath:      delivery.ArtifactPath,
			ArtifactName:      trimStringPtr(delivery.ArtifactName),
			ArtifactSizeBytes: delivery.ArtifactSizeBytes,
			ConsumerJobID:     delivery.ConsumerJobID,
			ConsumerJobName:   trimStringPtr(delivery.ConsumerJobName),
			DownstreamBuildID: trimStringPtr(delivery.DownstreamBuildID),
			ErrorMessage:      trimStringPtr(delivery.ErrorMessage),
		})
	}
	resolvedBuildID := strings.TrimSpace(buildID)
	if trimmedResponseBuildID := strings.TrimSpace(response.BuildID); trimmedResponseBuildID != "" {
		resolvedBuildID = trimmedResponseBuildID
	}
	return buildArtifactTriggerDeliveriesPayload{
		BuildID:                  resolvedBuildID,
		BuildTriggerKind:         strings.TrimSpace(response.BuildTriggerKind),
		RecursiveDispatchBlocked: response.RecursiveDispatchBlocked,
		Summary: buildArtifactTriggerSummaryView{
			DeliveryCount: response.Summary.DeliveryCount,
			QueuedCount:   response.Summary.QueuedCount,
			FailedCount:   response.Summary.FailedCount,
		},
		Deliveries: deliveries,
	}
}

func writeBuildArtifactTriggersHuman(w io.Writer, payload buildArtifactTriggerDeliveriesPayload) error {
	if _, err := fmt.Fprintf(w, "Artifact trigger deliveries for build %s\n", payload.BuildID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Trigger: %s\n", displayTriggerKind(payload.BuildTriggerKind)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Summary: %d deliveries, %d queued, %d failed\n", payload.Summary.DeliveryCount, payload.Summary.QueuedCount, payload.Summary.FailedCount); err != nil {
		return err
	}
	if len(payload.Deliveries) == 0 {
		if payload.RecursiveDispatchBlocked {
			_, err := fmt.Fprintln(w, "\nRecursive artifact-trigger dispatch is blocked for artifact-triggered builds.")
			return err
		}
		_, err := fmt.Fprintln(w, "\nNo artifact-trigger deliveries were recorded for this build.")
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "STATUS\tARTIFACT\tCONSUMER JOB\tDOWNSTREAM BUILD\tERROR"); err != nil {
		return err
	}
	for _, delivery := range payload.Deliveries {
		errorValue := "-"
		if delivery.ErrorMessage != nil {
			errorValue = *delivery.ErrorMessage
		}
		downstreamBuild := "-"
		if delivery.DownstreamBuildID != nil {
			downstreamBuild = *delivery.DownstreamBuildID
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", delivery.Status, displayArtifactTriggerArtifact(delivery), displayArtifactTriggerConsumerJob(delivery), downstreamBuild, errorValue); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func displayArtifactTriggerArtifact(delivery buildArtifactTriggerDeliveryView) string {
	if delivery.ArtifactName != nil {
		return *delivery.ArtifactName
	}
	if trimmed := strings.TrimSpace(delivery.ArtifactPath); trimmed != "" {
		return trimmed
	}
	return delivery.ArtifactID
}

func displayArtifactTriggerConsumerJob(delivery buildArtifactTriggerDeliveryView) string {
	if delivery.ConsumerJobName != nil {
		return *delivery.ConsumerJobName
	}
	return delivery.ConsumerJobID
}

func displayTriggerKind(kind string) string {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}
