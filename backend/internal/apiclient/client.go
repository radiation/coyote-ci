package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
)

const defaultTimeout = 10 * time.Second

type ErrorKind string

const (
	ErrorKindAuthentication ErrorKind = "authentication"
	ErrorKindAuthorization  ErrorKind = "authorization"
	ErrorKindNotFound       ErrorKind = "not_found"
	ErrorKindConflict       ErrorKind = "conflict"
	ErrorKindValidation     ErrorKind = "validation"
	ErrorKindTransport      ErrorKind = "transport"
	ErrorKindServer         ErrorKind = "server"
	ErrorKindUnexpected     ErrorKind = "unexpected"
)

type Error struct {
	Kind       ErrorKind
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(http.StatusText(e.StatusCode))
	}
	if e.RequestID == "" {
		return message
	}
	return fmt.Sprintf("%s (request_id=%s)", message, e.RequestID)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	token      string
	userAgent  string
}

func New(baseURL string, token string, userAgent string, httpClient *http.Client) (*Client, error) {
	parsed, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	} else if httpClient.Timeout == 0 {
		clone := *httpClient
		clone.Timeout = defaultTimeout
		httpClient = &clone
	}
	return &Client{
		baseURL:    parsed,
		httpClient: httpClient,
		token:      strings.TrimSpace(token),
		userAgent:  strings.TrimSpace(userAgent),
	}, nil
}

func (c *Client) GetMe(ctx context.Context) (api.MeResponse, error) {
	var envelope api.MeEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "api/me", nil, &envelope); err != nil {
		return api.MeResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) GetServerInfo(ctx context.Context) (api.ServerInfoResponse, error) {
	var envelope api.ServerInfoEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "api/info", nil, &envelope); err != nil {
		return api.ServerInfoResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) ListProjects(ctx context.Context) (api.ProjectListResponse, error) {
	var envelope api.ProjectListEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "api/projects", nil, &envelope); err != nil {
		return api.ProjectListResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) GetProject(ctx context.Context, selector string) (api.ProjectResponse, error) {
	var envelope api.ProjectEnvelope
	if err := c.doJSON(ctx, http.MethodGet, projectResourcePath(selector, ""), nil, &envelope); err != nil {
		return api.ProjectResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) ListJobs(ctx context.Context, projectSelector string) (api.JobListResponse, error) {
	var envelope api.JobListEnvelope
	if err := c.doJSON(ctx, http.MethodGet, projectResourcePath(projectSelector, "/jobs"), nil, &envelope); err != nil {
		return api.JobListResponse{}, err
	}
	return envelope.Data, nil
}

type GetJobOptions struct {
	Project string
}

func (c *Client) GetJob(ctx context.Context, selector string, options GetJobOptions) (api.JobResponse, error) {
	requestPath := jobResourcePath(selector, "")
	params := url.Values{}
	if trimmedProject := strings.TrimSpace(options.Project); trimmedProject != "" {
		params.Set("project", trimmedProject)
	}
	if encoded := params.Encode(); encoded != "" {
		requestPath += "?" + encoded
	}

	var envelope api.JobEnvelope
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &envelope); err != nil {
		return api.JobResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) GetBuild(ctx context.Context, buildID string) (api.BuildResponse, error) {
	var envelope api.BuildEnvelope
	if err := c.doJSON(ctx, http.MethodGet, buildResourcePath(buildID, ""), nil, &envelope); err != nil {
		return api.BuildResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) RerunBuild(ctx context.Context, buildID string) (api.BuildResponse, error) {
	var envelope api.BuildEnvelope
	if err := c.doJSON(ctx, http.MethodPost, buildResourcePath(buildID, "/rerun"), nil, &envelope); err != nil {
		return api.BuildResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) GetBuildSteps(ctx context.Context, buildID string) ([]api.BuildStepResponse, error) {
	var envelope api.BuildStepsEnvelope
	if err := c.doJSON(ctx, http.MethodGet, buildResourcePath(buildID, "/steps"), nil, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data.Steps, nil
}

type BuildLogsOptions struct {
	Step   *int
	Failed bool
	Tail   int
}

func (c *Client) GetBuildLogs(ctx context.Context, buildID string, options BuildLogsOptions) (api.BuildLogsResponse, error) {
	requestPath := buildResourcePath(buildID, "/logs")
	params := url.Values{}
	if options.Step != nil {
		params.Set("step", strconv.Itoa(*options.Step))
	}
	if options.Failed {
		params.Set("failed", "true")
	}
	if options.Tail > 0 {
		params.Set("tail", strconv.Itoa(options.Tail))
	}
	if encoded := params.Encode(); encoded != "" {
		requestPath += "?" + encoded
	}

	var envelope api.BuildLogsEnvelope
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &envelope); err != nil {
		return api.BuildLogsResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) ListBuildArtifacts(ctx context.Context, buildID string) (api.BuildArtifactsResponse, error) {
	var envelope api.BuildArtifactsEnvelope
	if err := c.doJSON(ctx, http.MethodGet, buildResourcePath(buildID, "/artifacts"), nil, &envelope); err != nil {
		return api.BuildArtifactsResponse{}, err
	}
	return envelope.Data, nil
}

func (c *Client) DownloadBuildArtifact(ctx context.Context, buildID string, artifactID string, writer io.Writer) error {
	requestURL, err := resolveRequestURL(c.baseURL, buildArtifactDownloadPath(buildID, artifactID))
	if err != nil {
		return &Error{Kind: ErrorKindUnexpected, Message: "invalid request path", Err: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return &Error{Kind: ErrorKindUnexpected, Message: "build request", Err: err}
	}
	request.Header.Set("Accept", "application/octet-stream")
	if c.userAgent != "" {
		request.Header.Set("User-Agent", c.userAgent)
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &Error{Kind: ErrorKindTransport, Message: "request failed", Err: err}
	}
	defer func() {
		_ = response.Body.Close()
	}()

	requestID := strings.TrimSpace(response.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(response.Header.Get("X-Request-ID"))
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeErrorResponse(response, requestID)
	}
	if writer == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if _, err := io.Copy(writer, response.Body); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &Error{Kind: ErrorKindUnexpected, Message: "stream artifact response", RequestID: requestID, Err: err}
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method string, path string, requestBody io.Reader, out any) error {
	requestURL, err := resolveRequestURL(c.baseURL, path)
	if err != nil {
		return &Error{Kind: ErrorKindUnexpected, Message: "invalid request path", Err: err}
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), requestBody)
	if err != nil {
		return &Error{Kind: ErrorKindUnexpected, Message: "build request", Err: err}
	}
	request.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		request.Header.Set("User-Agent", c.userAgent)
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &Error{Kind: ErrorKindTransport, Message: "request failed", Err: err}
	}
	defer func() {
		_ = response.Body.Close()
	}()

	requestID := strings.TrimSpace(response.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(response.Header.Get("X-Request-ID"))
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeErrorResponse(response, requestID)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if decodeErr := json.NewDecoder(response.Body).Decode(out); decodeErr != nil {
		return &Error{Kind: ErrorKindUnexpected, Message: "invalid json response", RequestID: requestID, Err: decodeErr}
	}
	return nil
}

func normalizeBaseURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("base url must include scheme and host")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("base url must use http or https")
	}
	if parsed.Fragment != "" {
		return nil, errors.New("base url must not include a fragment")
	}
	if parsed.User != nil {
		return nil, errors.New("base url must not include embedded credentials")
	}
	if parsed.RawQuery != "" {
		return nil, errors.New("base url must not include a query")
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path == "." || path == "/" {
		path = ""
	}
	parsed.Path = path
	parsed.RawPath = ""
	return parsed, nil
}

func resolveRequestURL(baseURL *url.URL, requestPath string) (*url.URL, error) {
	trimmed := strings.TrimSpace(requestPath)
	if trimmed == "" {
		return nil, errors.New("request path is required")
	}
	relative, err := url.Parse(strings.TrimPrefix(trimmed, "/"))
	if err != nil {
		return nil, err
	}
	if relative.Scheme != "" || relative.Host != "" || relative.User != nil || relative.Fragment != "" {
		return nil, errors.New("request path must be relative")
	}
	baseCopy := *baseURL
	basePath := strings.TrimSuffix(baseCopy.Path, "/")
	if basePath != "" {
		baseCopy.Path = basePath + "/"
	} else {
		baseCopy.Path = "/"
	}
	return baseCopy.ResolveReference(relative), nil
}

func buildResourcePath(buildID string, suffix string) string {
	return "api/builds/" + url.PathEscape(strings.TrimSpace(buildID)) + suffix
}

func projectResourcePath(selector string, suffix string) string {
	return "api/projects/" + url.PathEscape(strings.TrimSpace(selector)) + suffix
}

func jobResourcePath(selector string, suffix string) string {
	return "api/jobs/" + url.PathEscape(strings.TrimSpace(selector)) + suffix
}

func buildArtifactDownloadPath(buildID string, artifactID string) string {
	return buildResourcePath(buildID, "/artifacts/"+url.PathEscape(strings.TrimSpace(artifactID))+"/download")
}

func decodeErrorResponse(response *http.Response, requestID string) error {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
	if readErr != nil {
		return &Error{Kind: classifyStatus(response.StatusCode), StatusCode: response.StatusCode, Message: http.StatusText(response.StatusCode), RequestID: requestID, Err: readErr}
	}
	var payload api.ErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return &Error{
			Kind:       classifyStatus(response.StatusCode),
			StatusCode: response.StatusCode,
			Code:       strings.TrimSpace(payload.Error.Code),
			Message:    strings.TrimSpace(payload.Error.Message),
			RequestID:  requestID,
		}
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return &Error{Kind: classifyStatus(response.StatusCode), StatusCode: response.StatusCode, Message: message, RequestID: requestID}
}

func classifyStatus(statusCode int) ErrorKind {
	switch statusCode {
	case http.StatusUnauthorized:
		return ErrorKindAuthentication
	case http.StatusForbidden:
		return ErrorKindAuthorization
	case http.StatusNotFound:
		return ErrorKindNotFound
	case http.StatusConflict:
		return ErrorKindConflict
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrorKindValidation
	default:
		if statusCode >= 500 {
			return ErrorKindServer
		}
		return ErrorKindUnexpected
	}
}
