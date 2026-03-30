package api

type CreateBuildRequest struct {
	ProjectID string                 `json:"project_id"`
	Template  string                 `json:"template,omitempty"`
	Steps     []CreateBuildStepInput `json:"steps,omitempty"`
}

type QueueBuildRequest struct {
	Template string                `json:"template,omitempty"`
	Steps    []QueueBuildStepInput `json:"steps,omitempty"`
}

type QueueBuildStepInput struct {
	Name    string `json:"name,omitempty"`
	Command string `json:"command"`
}

type CreatePipelineBuildRequest struct {
	ProjectID    string `json:"project_id"`
	PipelineYAML string `json:"pipeline_yaml"`
}

type CreateRepoBuildRequest struct {
	ProjectID string `json:"project_id"`
	RepoURL   string `json:"repo_url"`
	Ref       string `json:"ref"`
}

type CreateBuildStepInput struct {
	Name           string            `json:"name"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type DataResponse struct {
	Data any `json:"data"`
}

type BuildEnvelope struct {
	Data BuildResponse `json:"data"`
}

type BuildListEnvelope struct {
	Data BuildListResponse `json:"data"`
}

type BuildStepsEnvelope struct {
	Data BuildStepsResponse `json:"data"`
}

type BuildLogsEnvelope struct {
	Data BuildLogsResponse `json:"data"`
}

type BuildArtifactsEnvelope struct {
	Data BuildArtifactsResponse `json:"data"`
}

type StepLogsEnvelope struct {
	Data StepLogsResponse `json:"data"`
}

type BuildResponse struct {
	ID                 string  `json:"id"`
	ProjectID          string  `json:"project_id"`
	Status             string  `json:"status"`
	CreatedAt          string  `json:"created_at"`
	QueuedAt           *string `json:"queued_at"`
	StartedAt          *string `json:"started_at"`
	FinishedAt         *string `json:"finished_at"`
	CurrentStepIndex   int     `json:"current_step_index"`
	ErrorMessage       *string `json:"error_message"`
	PipelineConfigYAML *string `json:"pipeline_config_yaml,omitempty"`
	PipelineName       *string `json:"pipeline_name,omitempty"`
	PipelineSource     *string `json:"pipeline_source,omitempty"`
	RepoURL            *string `json:"repo_url,omitempty"`
	Ref                *string `json:"ref,omitempty"`
	CommitSHA          *string `json:"commit_sha,omitempty"`
}

type BuildListResponse struct {
	Builds []BuildResponse `json:"builds"`
}

type BuildStepResponse struct {
	ID           string  `json:"id"`
	BuildID      string  `json:"build_id"`
	StepIndex    int     `json:"step_index"`
	Name         string  `json:"name"`
	Command      string  `json:"command"`
	Status       string  `json:"status"`
	WorkerID     *string `json:"worker_id"`
	StartedAt    *string `json:"started_at"`
	FinishedAt   *string `json:"finished_at"`
	ExitCode     *int    `json:"exit_code"`
	Stdout       *string `json:"stdout"`
	Stderr       *string `json:"stderr"`
	ErrorMessage *string `json:"error_message"`
}

type BuildStepsResponse struct {
	BuildID string              `json:"build_id"`
	Steps   []BuildStepResponse `json:"steps"`
}

type BuildLogResponse struct {
	StepName  string `json:"step_name"`
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

type BuildLogsResponse struct {
	BuildID string             `json:"build_id"`
	Logs    []BuildLogResponse `json:"logs"`
}

type StepLogChunkResponse struct {
	SequenceNo int64  `json:"sequence_no"`
	BuildID    string `json:"build_id"`
	StepID     string `json:"step_id"`
	StepIndex  int    `json:"step_index"`
	StepName   string `json:"step_name"`
	Stream     string `json:"stream"`
	ChunkText  string `json:"chunk_text"`
	CreatedAt  string `json:"created_at"`
}

type StepLogsResponse struct {
	BuildID      string                 `json:"build_id"`
	StepIndex    int                    `json:"step_index"`
	After        int64                  `json:"after"`
	NextSequence int64                  `json:"next_sequence"`
	Chunks       []StepLogChunkResponse `json:"chunks"`
}

type BuildArtifactResponse struct {
	ID              string  `json:"id"`
	BuildID         string  `json:"build_id"`
	Path            string  `json:"path"`
	SizeBytes       int64   `json:"size_bytes"`
	ContentType     *string `json:"content_type"`
	ChecksumSHA256  *string `json:"checksum_sha256"`
	DownloadURLPath string  `json:"download_url_path"`
	CreatedAt       string  `json:"created_at"`
}

type BuildArtifactsResponse struct {
	BuildID   string                  `json:"build_id"`
	Artifacts []BuildArtifactResponse `json:"artifacts"`
}
