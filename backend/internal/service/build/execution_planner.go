package build

import (
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type BuildExecutionPlanner struct {
	specVersion int
	clock       func() time.Time
}

func NewBuildExecutionPlanner() *BuildExecutionPlanner {
	return &BuildExecutionPlanner{
		specVersion: 1,
		clock:       time.Now,
	}
}

func (p *BuildExecutionPlanner) Plan(build domain.Build, steps []domain.BuildStep, image string) ([]domain.ExecutionJob, error) {
	if len(steps) == 0 {
		return []domain.ExecutionJob{}, nil
	}

	resolvedImage := strings.TrimSpace(image)
	contextDir := plannerContextDirFromPipelinePath(build.PipelinePath)
	pipelinePath := optionalValue(build.PipelinePath)
	sourceRef := plannerSourceRef(build.Source)
	triggerEnv := plannerTriggerEnv(build.Trigger)
	workspacePlans := planWorkspaceInputs(steps)

	jobs := make([]domain.ExecutionJob, 0, len(steps))
	for stepIndex, step := range steps {
		// Step-level image overrides pipeline-level/default image.
		stepImage := strings.TrimSpace(step.Image)
		if stepImage == "" {
			stepImage = resolvedImage
		}

		timeout := step.TimeoutSeconds
		spec := domain.ExecutionJobSpec{
			Version:          p.specVersion,
			Image:            stepImage,
			WorkingDir:       defaultValue(step.WorkingDir, "."),
			Command:          append([]string{defaultValue(step.Command, "sh")}, append([]string(nil), step.Args...)...),
			Environment:      mergePlannerEnv(step.Env, triggerEnv),
			TimeoutSeconds:   maxInt(step.TimeoutSeconds, 0),
			PipelineFilePath: pipelinePath,
			ContextDir:       contextDir,
			Source: domain.SourceSnapshotRef{
				RepositoryURL: plannerSourceRepositoryURL(build.Source, build.RepoURL),
				CommitSHA:     plannerSourceCommitSHA(build.Source, build.CommitSHA),
				RefName:       sourceRef,
			},
			WorkspaceInput: workspacePlans[stepIndex],
		}

		specJSON, err := spec.ToJSON()
		if err != nil {
			return nil, err
		}

		jobID := uuid.NewString()
		var groupName *string
		if step.GroupName != nil {
			trimmed := strings.TrimSpace(*step.GroupName)
			if trimmed != "" {
				groupName = &trimmed
			}
		}
		jobs = append(jobs, domain.ExecutionJob{
			ID:               jobID,
			BuildID:          build.ID,
			StepID:           step.ID,
			NodeID:           step.NodeID,
			GroupName:        groupName,
			DependsOnNodeIDs: append([]string(nil), step.DependsOnNodes...),
			Name:             step.Name,
			StepIndex:        step.StepIndex,
			AttemptNumber:    1,
			LineageRootJobID: &jobID,
			Status:           domain.ExecutionJobStatusQueued,
			Image:            stepImage,
			WorkingDir:       spec.WorkingDir,
			Command:          spec.Command,
			Environment:      spec.Environment,
			TimeoutSeconds:   &timeout,
			PipelineFilePath: optionalPointer(pipelinePath),
			ContextDir:       optionalPointer(contextDir),
			Source:           spec.Source,
			SpecVersion:      p.specVersion,
			SpecDigest:       domain.BuildSpecDigest(specJSON),
			ResolvedSpecJSON: specJSON,
			CreatedAt:        p.clock().UTC(),
			OutputRefs:       []domain.ArtifactRef{},
		})
	}

	return jobs, nil
}

func planWorkspaceInputs(steps []domain.BuildStep) []domain.WorkspaceInputPlan {
	plans := make([]domain.WorkspaceInputPlan, len(steps))
	stepIndexByNodeID := make(map[string]int, len(steps))
	dependenciesByNodeID := make(map[string][]string, len(steps))
	dependentCountByNodeID := make(map[string]int, len(steps))

	for index, step := range steps {
		nodeID := strings.TrimSpace(step.NodeID)
		if nodeID == "" {
			continue
		}
		stepIndexByNodeID[nodeID] = index
	}

	for _, step := range steps {
		nodeID := strings.TrimSpace(step.NodeID)
		if nodeID == "" {
			continue
		}
		dependencies := uniqueKnownDependencies(step.DependsOnNodes, stepIndexByNodeID)
		dependenciesByNodeID[nodeID] = dependencies
		for _, dependencyNodeID := range dependencies {
			dependentCountByNodeID[dependencyNodeID]++
		}
	}

	for index, step := range steps {
		nodeID := strings.TrimSpace(step.NodeID)
		dependencies := dependenciesByNodeID[nodeID]
		switch len(dependencies) {
		case 0:
			plans[index] = domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource}
		case 1:
			producerNodeID := dependencies[0]
			plans[index] = domain.WorkspaceInputPlan{
				Mode:                       domain.WorkspaceInputModePredecessor,
				ProducerNodeID:             producerNodeID,
				IsolatedWritableDescendant: dependentCountByNodeID[producerNodeID] > 1,
			}
		default:
			plans[index] = domain.WorkspaceInputPlan{
				Mode:                 domain.WorkspaceInputModeFanIn,
				CommonAncestorNodeID: nearestCommonAncestor(dependencies, dependenciesByNodeID),
			}
		}
	}

	return plans
}

func uniqueKnownDependencies(dependencies []string, knownNodeIDs map[string]int) []string {
	seen := make(map[string]struct{}, len(dependencies))
	result := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		nodeID := strings.TrimSpace(dependency)
		if nodeID == "" {
			continue
		}
		if _, known := knownNodeIDs[nodeID]; !known {
			continue
		}
		if _, duplicate := seen[nodeID]; duplicate {
			continue
		}
		seen[nodeID] = struct{}{}
		result = append(result, nodeID)
	}
	return result
}

func nearestCommonAncestor(dependencies []string, dependenciesByNodeID map[string][]string) string {
	if len(dependencies) == 0 {
		return ""
	}

	common := ancestorNodeIDs(dependencies[0], dependenciesByNodeID)
	for _, dependency := range dependencies[1:] {
		ancestors := ancestorNodeIDs(dependency, dependenciesByNodeID)
		for candidate := range common {
			if _, found := ancestors[candidate]; !found {
				delete(common, candidate)
			}
		}
	}

	nearest := ""
	nearestDepth := -1
	depthByNodeID := make(map[string]int, len(common))
	for candidate := range common {
		candidateDepth := nodeDepth(candidate, dependenciesByNodeID, depthByNodeID)
		if candidateDepth > nearestDepth || (candidateDepth == nearestDepth && candidate < nearest) {
			nearest = candidate
			nearestDepth = candidateDepth
		}
	}
	return nearest
}

func nodeDepth(nodeID string, dependenciesByNodeID map[string][]string, depthByNodeID map[string]int) int {
	if depth, found := depthByNodeID[nodeID]; found {
		return depth
	}

	depth := 0
	for _, dependencyNodeID := range dependenciesByNodeID[nodeID] {
		candidateDepth := nodeDepth(dependencyNodeID, dependenciesByNodeID, depthByNodeID) + 1
		if candidateDepth > depth {
			depth = candidateDepth
		}
	}
	depthByNodeID[nodeID] = depth
	return depth
}

func ancestorNodeIDs(nodeID string, dependenciesByNodeID map[string][]string) map[string]struct{} {
	ancestors := map[string]struct{}{}
	stack := []string{nodeID}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, visited := ancestors[current]; visited {
			continue
		}
		ancestors[current] = struct{}{}
		stack = append(stack, dependenciesByNodeID[current]...)
	}
	return ancestors
}

func plannerSourceRepositoryURL(spec *domain.SourceSpec, fallback *string) string {
	if spec != nil {
		return strings.TrimSpace(spec.RepositoryURL)
	}
	if fallback == nil {
		return ""
	}
	return strings.TrimSpace(*fallback)
}

func plannerSourceCommitSHA(spec *domain.SourceSpec, fallback *string) string {
	if spec != nil && spec.CommitSHA != nil {
		return strings.TrimSpace(*spec.CommitSHA)
	}
	if fallback == nil {
		return ""
	}
	return strings.TrimSpace(*fallback)
}

func plannerSourceRef(spec *domain.SourceSpec) *string {
	if spec == nil || spec.Ref == nil {
		return nil
	}
	value := strings.TrimSpace(*spec.Ref)
	if value == "" {
		return nil
	}
	return &value
}

func plannerContextDirFromPipelinePath(pipelinePath *string) string {
	if pipelinePath == nil {
		return "."
	}
	normalized := strings.TrimSpace(*pipelinePath)
	if normalized == "" {
		return "."
	}
	dir := path.Clean(path.Dir(strings.ReplaceAll(normalized, "\\", "/")))
	if dir == "" {
		return "."
	}
	return dir
}

func cloneEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	return out
}

func mergePlannerEnv(base map[string]string, extra map[string]string) map[string]string {
	merged := cloneEnv(base)
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func plannerTriggerEnv(trigger domain.BuildTrigger) map[string]string {
	trigger = domain.NormalizeBuildTrigger(trigger)
	if trigger.Kind != domain.BuildTriggerKindArtifact {
		return nil
	}
	env := map[string]string{
		"COYOTE_TRIGGER_TYPE": "artifact_uploaded",
	}
	if trigger.ProducerProjectID != nil {
		env["COYOTE_TRIGGER_PROJECT_ID"] = *trigger.ProducerProjectID
	}
	if trigger.ProducerJobID != nil {
		env["COYOTE_TRIGGER_JOB_ID"] = *trigger.ProducerJobID
	}
	if trigger.ProducerBuildID != nil {
		env["COYOTE_TRIGGER_BUILD_ID"] = *trigger.ProducerBuildID
	}
	if trigger.ArtifactID != nil {
		env["COYOTE_TRIGGER_ARTIFACT_ID"] = *trigger.ArtifactID
	}
	if trigger.ArtifactPath != nil {
		env["COYOTE_TRIGGER_ARTIFACT_PATH"] = *trigger.ArtifactPath
	}
	if trigger.ArtifactName != nil {
		env["COYOTE_TRIGGER_ARTIFACT_NAME"] = *trigger.ArtifactName
	}
	if trigger.ArtifactSizeBytes != nil {
		env["COYOTE_TRIGGER_ARTIFACT_SIZE_BYTES"] = strconv.FormatInt(*trigger.ArtifactSizeBytes, 10)
	}
	if trigger.ArtifactChecksumSHA256 != nil {
		env["COYOTE_TRIGGER_ARTIFACT_CHECKSUM_SHA256"] = *trigger.ArtifactChecksumSHA256
	}
	return env
}

func optionalPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func defaultValue(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
