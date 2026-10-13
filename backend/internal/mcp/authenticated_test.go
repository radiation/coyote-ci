package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestAuthenticatedMCPGetBuildTool(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	server := httptest.NewServer(fixture.handler(t, false))
	defer server.Close()

	tests := []struct {
		name      string
		token     string
		wantError string
	}{
		{name: "viewer", token: fixture.tokens["viewer"]},
		{name: "maintainer", token: fixture.tokens["maintainer"]},
		{name: "owner", token: fixture.tokens["owner"]},
		{name: "global admin", token: fixture.tokens["admin"]},
		{name: "missing project membership", token: fixture.tokens["outsider"], wantError: "forbidden"},
		{name: "missing build scope", token: fixture.tokens["missing_scope"], wantError: "missing_token_scope"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			session := connectMCPClient(t, server.URL+Path, tc.token)
			defer func() { _ = session.Close() }()

			tools, listErr := session.ListTools(context.Background(), nil)
			if listErr != nil {
				t.Fatalf("list tools failed: %v", listErr)
			}
			if !containsTool(tools.Tools, GetBuildToolName) {
				t.Fatalf("expected %q in tool list: %+v", GetBuildToolName, tools.Tools)
			}

			result, callErr := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name:      GetBuildToolName,
				Arguments: map[string]any{"build_id": fixture.build.ID},
			})
			if callErr != nil {
				t.Fatalf("call tool failed: %v", callErr)
			}
			if tc.wantError != "" {
				if !result.IsError || toolResultText(result) != tc.wantError {
					t.Fatalf("expected tool error %q, got %+v", tc.wantError, result)
				}
				return
			}
			if result.IsError {
				t.Fatalf("expected success, got %+v", result)
			}
			structuredJSON, structuredErr := json.Marshal(result.StructuredContent)
			if structuredErr != nil {
				t.Fatalf("marshal structured output: %v", structuredErr)
			}
			var output GetBuildOutput
			if decodeErr := json.Unmarshal(structuredJSON, &output); decodeErr != nil {
				t.Fatalf("decode structured output: %v", decodeErr)
			}
			if output.BuildID != fixture.build.ID || output.ProjectID != fixture.build.ProjectID || output.Status != fixture.build.Status {
				t.Fatalf("unexpected output: %+v", output)
			}
			encoded, marshalErr := json.Marshal(output)
			if marshalErr != nil {
				t.Fatalf("marshal output: %v", marshalErr)
			}
			if strings.Contains(string(encoded), "pipeline-secret") || strings.Contains(string(encoded), "workspace-path") {
				t.Fatalf("unsafe output fields leaked: %s", encoded)
			}
		})
	}
}

func TestAuthenticatedMCPRejectsInvalidCredentials(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	handler := fixture.handler(t, false)
	tests := []struct {
		name  string
		token string
	}{
		{name: "missing"},
		{name: "malformed", token: "Bearer invalid"},
		{name: "invalid", token: "Bearer coyote_pat_not-a-real-token"},
		{name: "expired", token: "Bearer " + fixture.expiredToken},
		{name: "revoked", token: "Bearer " + fixture.revokedToken},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"test"}}}`))
			request.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				request.Header.Set("Authorization", tc.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusUnauthorized, response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), fixture.revokedToken) || strings.Contains(response.Body.String(), fixture.expiredToken) {
				t.Fatalf("authentication response leaked token material: %q", response.Body.String())
			}
		})
	}
}

func TestAuthenticatedMCPHandlerAuthenticationFailureDiagnostics(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	const sensitiveToken = "coyote_pat_do_not_log_this_value"

	repositoryFailure := errors.New("database query failed for coyote_pat_do_not_log_this_value")
	repositoryFailureService := service.NewAPITokenService(failingGetByHashAPITokenRepository{
		APITokenRepository: repositorymemory.NewAPITokenRepository(),
		err:                repositoryFailure,
	}, repositorymemory.NewUserRepository())

	ownerTokenRepo := repositorymemory.NewAPITokenRepository()
	now := time.Now().UTC()
	_, createErr := ownerTokenRepo.Create(context.Background(), domain.APIToken{
		ID:        "token-owner-missing",
		UserID:    "owner-missing",
		Name:      "owner-missing",
		TokenHash: service.HashAPIToken(sensitiveToken),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if createErr != nil {
		t.Fatalf("create owner lookup token fixture: %v", createErr)
	}
	ownerFailureService := service.NewAPITokenService(ownerTokenRepo, repositorymemory.NewUserRepository())

	tests := []struct {
		name       string
		tokens     *service.APITokenService
		token      string
		wantStatus int
		wantBody   string
		wantReason string
	}{
		{
			name:       "valid token",
			tokens:     fixture.apiTokens,
			token:      fixture.tokens["viewer"],
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid token",
			tokens:     fixture.apiTokens,
			token:      sensitiveToken,
			wantStatus: http.StatusUnauthorized,
			wantBody:   "invalid api token\n",
			wantReason: string(apiTokenAuthenticationFailureTokenNotFound),
		},
		{
			name:       "expired token",
			tokens:     fixture.apiTokens,
			token:      fixture.expiredToken,
			wantStatus: http.StatusUnauthorized,
			wantBody:   "invalid api token\n",
			wantReason: string(apiTokenAuthenticationFailureTokenExpired),
		},
		{
			name:       "revoked token",
			tokens:     fixture.apiTokens,
			token:      fixture.revokedToken,
			wantStatus: http.StatusUnauthorized,
			wantBody:   "invalid api token\n",
			wantReason: string(apiTokenAuthenticationFailureTokenRevoked),
		},
		{
			name:       "repository failure",
			tokens:     repositoryFailureService,
			token:      sensitiveToken,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "authentication service unavailable\n",
			wantReason: string(apiTokenAuthenticationFailureRepository),
		},
		{
			name:       "owner resolution failure",
			tokens:     ownerFailureService,
			token:      sensitiveToken,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "authentication service unavailable\n",
			wantReason: string(apiTokenAuthenticationFailureOwner),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(previousOutput)

			handler := fixture.handlerWithTokens(t, tc.tokens, false)
			request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"test"}}}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			request.Header.Set("Authorization", "Bearer "+tc.token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantStatus, response.Code, response.Body.String())
			}
			if tc.wantBody != "" && response.Body.String() != tc.wantBody {
				t.Fatalf("expected body %q, got %q", tc.wantBody, response.Body.String())
			}
			logged := output.String()
			if tc.wantReason == "" {
				if logged != "" {
					t.Fatalf("expected no success diagnostic, got %q", logged)
				}
			} else if !strings.Contains(logged, "MCP API token authentication failed: reason="+tc.wantReason) {
				t.Fatalf("expected failure classification %q in logs, got %q", tc.wantReason, logged)
			}
			if strings.Contains(logged, tc.token) || strings.Contains(logged, sensitiveToken) || strings.Contains(logged, repositoryFailure.Error()) {
				t.Fatalf("authentication diagnostics leaked sensitive material: %q", logged)
			}
		})
	}
}

func TestPATAuthenticationMapsAuthenticationFailuresSafely(t *testing.T) {
	const sensitiveToken = "coyote_pat_do_not_log_this_value"
	repositoryFailure := errors.New("database query failed for coyote_pat_do_not_log_this_value")
	tokenRepo := failingGetByHashAPITokenRepository{
		APITokenRepository: repositorymemory.NewAPITokenRepository(),
		err:                repositoryFailure,
	}
	repositoryFailureService := service.NewAPITokenService(tokenRepo, repositorymemory.NewUserRepository())

	ownerTokenRepo := repositorymemory.NewAPITokenRepository()
	now := time.Now().UTC()
	_, createErr := ownerTokenRepo.Create(context.Background(), domain.APIToken{
		ID:        "token-owner-missing",
		UserID:    "owner-missing",
		Name:      "owner-missing",
		TokenHash: service.HashAPIToken(sensitiveToken),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if createErr != nil {
		t.Fatalf("create owner lookup token fixture: %v", createErr)
	}
	ownerFailureService := service.NewAPITokenService(ownerTokenRepo, repositorymemory.NewUserRepository())

	tests := []struct {
		name       string
		tokens     *service.APITokenService
		wantStatus int
		wantBody   string
		wantLog    apiTokenAuthenticationFailure
	}{
		{
			name:       "repository failure",
			tokens:     repositoryFailureService,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "authentication service unavailable\n",
			wantLog:    apiTokenAuthenticationFailureRepository,
		},
		{
			name:       "owner resolution failure",
			tokens:     ownerFailureService,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "authentication service unavailable\n",
			wantLog:    apiTokenAuthenticationFailureOwner,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(previousOutput)

			handler := withPATAuthentication(tc.tokens)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("expected authentication to stop the request")
			}))
			request := httptest.NewRequest(http.MethodPost, Path, nil)
			request.Header.Set("Authorization", "Bearer "+sensitiveToken)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, response.Code)
			}
			if response.Body.String() != tc.wantBody {
				t.Fatalf("expected safe response %q, got %q", tc.wantBody, response.Body.String())
			}
			if !strings.Contains(logs.String(), "reason="+string(tc.wantLog)) {
				t.Fatalf("expected classification %q in logs, got %q", tc.wantLog, logs.String())
			}
			if strings.Contains(response.Body.String(), sensitiveToken) || strings.Contains(logs.String(), sensitiveToken) {
				t.Fatal("authentication failure leaked token material")
			}
		})
	}
}

func TestClassifyAPITokenAuthenticationFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want apiTokenAuthenticationFailure
	}{
		{name: "invalid format", err: service.ErrAPITokenInvalidFormat, want: apiTokenAuthenticationFailureInvalidFormat},
		{name: "not found", err: service.ErrAPITokenNotFound, want: apiTokenAuthenticationFailureTokenNotFound},
		{name: "revoked", err: service.ErrAPITokenRevoked, want: apiTokenAuthenticationFailureTokenRevoked},
		{name: "expired", err: service.ErrAPITokenExpired, want: apiTokenAuthenticationFailureTokenExpired},
		{name: "repository", err: service.ErrAPITokenRepositoryFailure, want: apiTokenAuthenticationFailureRepository},
		{name: "owner", err: service.ErrAPITokenOwnerResolutionFailure, want: apiTokenAuthenticationFailureOwner},
		{name: "unexpected", err: errors.New("unexpected"), want: apiTokenAuthenticationFailureUnexpected},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyAPITokenAuthenticationFailure(tc.err); got != tc.want {
				t.Fatalf("expected classification %q, got %q", tc.want, got)
			}
		})
	}
}

func TestPATAuthenticationPreservesPrincipalIdentityAndScopes(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	var principal authorization.Principal
	handler := withPATAuthentication(fixture.apiTokens)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		var ok bool
		principal, ok = currentPrincipal(request.Context())
		if !ok {
			t.Fatal("expected principal in request context")
		}
	}))
	request := httptest.NewRequest(http.MethodPost, Path, nil)
	request.Header.Set("Authorization", "Bearer "+fixture.tokens["viewer"])
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if principal.User.ID != "viewer-1" || principal.APIToken == nil {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if !domain.HasAPITokenScope(principal.APIToken.Scopes, domain.APITokenScopeBuildRead) {
		t.Fatalf("expected build:read scope, got %v", principal.APIToken.Scopes)
	}
	principal.APIToken.Scopes[0] = domain.APITokenScopeBuildLogs
	authenticated, err := fixture.apiTokens.AuthenticateAPIToken(context.Background(), fixture.tokens["viewer"])
	if err != nil {
		t.Fatalf("reauthenticate token: %v", err)
	}
	if !domain.HasAPITokenScope(authenticated.Token.Scopes, domain.APITokenScopeBuildRead) {
		t.Fatalf("request principal mutation altered persisted token scopes: %v", authenticated.Token.Scopes)
	}
}

func TestAuthenticatedMCPGetBuildToolReturnsSafeErrors(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	server := httptest.NewServer(fixture.handler(t, false))
	defer server.Close()

	session := connectMCPClient(t, server.URL+Path, fixture.tokens["viewer"])
	defer func() { _ = session.Close() }()
	for _, tc := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{name: "missing build ID", arguments: map[string]any{}},
		{name: "unknown build", arguments: map[string]any{"build_id": "missing"}, want: "not_found"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: GetBuildToolName, Arguments: tc.arguments})
			if err != nil {
				t.Fatalf("call tool failed: %v", err)
			}
			if !result.IsError {
				t.Fatalf("expected tool error, got %+v", result)
			}
			if tc.want != "" && toolResultText(result) != tc.want {
				t.Fatalf("expected safe tool error %q, got %+v", tc.want, result)
			}
		})
	}
}

func TestAuthenticatedMCPBuildInvestigationTools(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	server := httptest.NewServer(fixture.handler(t, false))
	defer server.Close()

	logReader := connectMCPClient(t, server.URL+Path, fixture.tokens["log_reader"])
	defer func() { _ = logReader.Close() }()
	tools, listErr := logReader.ListTools(context.Background(), nil)
	if listErr != nil {
		t.Fatalf("list tools failed: %v", listErr)
	}
	for _, toolName := range []string{GetBuildFailuresToolName, GetStepLogsToolName} {
		if !containsTool(tools.Tools, toolName) {
			t.Fatalf("expected %q in tool list: %+v", toolName, tools.Tools)
		}
	}

	failuresResult, failuresErr := logReader.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      GetBuildFailuresToolName,
		Arguments: map[string]any{"build_id": fixture.build.ID},
	})
	if failuresErr != nil {
		t.Fatalf("call failures tool: %v", failuresErr)
	}
	if failuresResult.IsError {
		t.Fatalf("expected failure metadata success, got %+v", failuresResult)
	}
	failuresJSON, marshalFailuresErr := json.Marshal(failuresResult.StructuredContent)
	if marshalFailuresErr != nil {
		t.Fatalf("marshal failures output: %v", marshalFailuresErr)
	}
	var failures GetBuildFailuresOutput
	if decodeFailuresErr := json.Unmarshal(failuresJSON, &failures); decodeFailuresErr != nil {
		t.Fatalf("decode failures output: %v", decodeFailuresErr)
	}
	if failures.Status != domain.BuildStatusFailed || len(failures.FailedSteps) != 1 || failures.FailedSteps[0].StepIndex != 0 {
		t.Fatalf("unexpected failures output: %+v", failures)
	}
	encodedFailures, encodeFailuresErr := json.Marshal(failures)
	if encodeFailuresErr != nil {
		t.Fatalf("marshal decoded failures: %v", encodeFailuresErr)
	}
	if strings.Contains(string(encodedFailures), "pipeline-secret") || strings.Contains(string(encodedFailures), "first log") {
		t.Fatalf("failure output leaked unsafe content: %s", encodedFailures)
	}

	logResult, logErr := logReader.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: GetStepLogsToolName,
		Arguments: map[string]any{
			"build_id":       fixture.build.ID,
			"step_index":     0,
			"limit":          1,
			"max_bytes":      64,
			"after_sequence": 0,
		},
	})
	if logErr != nil {
		t.Fatalf("call logs tool: %v", logErr)
	}
	if logResult.IsError {
		t.Fatalf("expected logs success, got %+v", logResult)
	}
	logsJSON, marshalLogsErr := json.Marshal(logResult.StructuredContent)
	if marshalLogsErr != nil {
		t.Fatalf("marshal logs output: %v", marshalLogsErr)
	}
	var output GetStepLogsOutput
	if decodeLogsErr := json.Unmarshal(logsJSON, &output); decodeLogsErr != nil {
		t.Fatalf("decode logs output: %v", decodeLogsErr)
	}
	if len(output.Chunks) != 1 || output.Chunks[0].Text != "first log\n" || !output.HasMore || output.NextSequence != output.Chunks[0].Sequence {
		t.Fatalf("unexpected paginated logs output: %+v", output)
	}

	viewer := connectMCPClient(t, server.URL+Path, fixture.tokens["viewer"])
	defer func() { _ = viewer.Close() }()
	missingScopeResult, missingScopeErr := viewer.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      GetStepLogsToolName,
		Arguments: map[string]any{"build_id": fixture.build.ID, "step_index": 0},
	})
	if missingScopeErr != nil {
		t.Fatalf("call logs without scope: %v", missingScopeErr)
	}
	if !missingScopeResult.IsError || toolResultText(missingScopeResult) != "missing_token_scope" {
		t.Fatalf("expected missing scope error, got %+v", missingScopeResult)
	}

	tooSmallResult, tooSmallErr := logReader.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      GetStepLogsToolName,
		Arguments: map[string]any{"build_id": fixture.build.ID, "step_index": 0, "max_bytes": 1},
	})
	if tooSmallErr != nil {
		t.Fatalf("call logs with too-small byte budget: %v", tooSmallErr)
	}
	if !tooSmallResult.IsError || toolResultText(tooSmallResult) != "chunk_exceeds_limit" {
		t.Fatalf("expected oversized chunk error, got %+v", tooSmallResult)
	}
}

func TestAuthenticatedMCPStepLogsRejectInvalidInput(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	server := httptest.NewServer(fixture.handler(t, false))
	defer server.Close()

	session := connectMCPClient(t, server.URL+Path, fixture.tokens["log_reader"])
	defer func() { _ = session.Close() }()
	for _, arguments := range []map[string]any{
		{"build_id": fixture.build.ID},
		{"build_id": fixture.build.ID, "step_index": -1},
		{"build_id": fixture.build.ID, "step_index": 0, "after_sequence": -1},
		{"build_id": fixture.build.ID, "step_index": 0, "limit": 501},
		{"build_id": fixture.build.ID, "step_index": 0, "max_bytes": 262145},
		{"build_id": fixture.build.ID, "step_index": 99},
	} {
		result, callErr := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: GetStepLogsToolName, Arguments: arguments})
		if callErr != nil {
			t.Fatalf("call logs tool: %v", callErr)
		}
		if !result.IsError {
			t.Fatalf("expected invalid input error, got %+v", result)
		}
	}
}

func TestAuthenticatedMCPRequestsRemainPrincipalScoped(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	server := httptest.NewServer(fixture.handler(t, false))
	defer server.Close()

	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, token := range []string{fixture.tokens["viewer"], fixture.tokens["outsider"]} {
		token := token
		wg.Add(1)
		go func() {
			defer wg.Done()
			session := connectMCPClient(t, server.URL+Path, token)
			defer func() { _ = session.Close() }()
			result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name:      GetBuildToolName,
				Arguments: map[string]any{"build_id": fixture.build.ID},
			})
			if err != nil {
				results <- "client_error"
				return
			}
			if result.IsError {
				results <- toolResultText(result)
				return
			}
			results <- "success"
		}()
	}
	wg.Wait()
	close(results)

	got := map[string]bool{}
	for result := range results {
		got[result] = true
	}
	if !got["success"] || !got["forbidden"] {
		t.Fatalf("expected separately scoped results, got %v", got)
	}
}

func TestAuthenticatedMCPDisabledModeRequiresExplicitDependency(t *testing.T) {
	fixture := newAuthenticatedMCPFixture(t)
	server := httptest.NewServer(fixture.handler(t, true))
	defer server.Close()

	session := connectMCPClient(t, server.URL+Path, "")
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      GetBuildToolName,
		Arguments: map[string]any{"build_id": fixture.build.ID},
	})
	if err != nil {
		t.Fatalf("call tool failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected explicit disabled mode to permit local development access: %+v", result)
	}
}

type authenticatedMCPFixture struct {
	build        domain.Build
	tokens       map[string]string
	expiredToken string
	revokedToken string
	apiTokens    *service.APITokenService
	buildService *buildsvc.BuildService
	reader       *buildsvc.AuthorizedReader
}

func newAuthenticatedMCPFixture(t *testing.T) authenticatedMCPFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	userRepo := repositorymemory.NewUserRepository()
	membershipRepo := repositorymemory.NewProjectMembershipRepository(projectRepo, userRepo)
	tokenRepo := repositorymemory.NewAPITokenRepository()
	buildRepo := repositorymemory.NewBuildRepository()
	if _, projectErr := projectRepo.Create(ctx, domain.Project{ID: "project-1", Name: "Project", Slug: "project", CreatedAt: now, UpdatedAt: now}); projectErr != nil {
		t.Fatalf("create project failed: %v", projectErr)
	}

	users := map[string]domain.User{
		"viewer":        {ID: "viewer-1", Email: "viewer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
		"maintainer":    {ID: "maintainer-1", Email: "maintainer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
		"owner":         {ID: "owner-1", Email: "owner@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
		"outsider":      {ID: "outsider-1", Email: "outsider@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
		"admin":         {ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin, CreatedAt: now, UpdatedAt: now},
		"missing_scope": {ID: "missing-scope-1", Email: "scope@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
		"log_reader":    {ID: "log-reader-1", Email: "logs@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
	}
	for name, user := range users {
		if _, userErr := userRepo.Create(ctx, user); userErr != nil {
			t.Fatalf("create %s user failed: %v", name, userErr)
		}
	}
	for name, role := range map[string]domain.ProjectMemberRole{
		"viewer": domain.ProjectMemberRoleViewer, "maintainer": domain.ProjectMemberRoleMaintainer, "owner": domain.ProjectMemberRoleOwner, "missing_scope": domain.ProjectMemberRoleViewer, "log_reader": domain.ProjectMemberRoleViewer,
	} {
		if _, membershipErr := membershipRepo.Upsert(ctx, domain.ProjectMembership{ProjectID: "project-1", UserID: users[name].ID, Role: role, CreatedAt: now, UpdatedAt: now}); membershipErr != nil {
			t.Fatalf("create %s membership failed: %v", name, membershipErr)
		}
	}
	build := domain.Build{
		ID:                 "build-1",
		BuildNumber:        42,
		ProjectID:          "project-1",
		Status:             domain.BuildStatusFailed,
		CreatedAt:          now,
		ErrorMessage:       stringPointer("pipeline-secret workspace-path"),
		PipelineConfigYAML: stringPointer("pipeline-secret"),
	}
	if _, buildErr := buildRepo.CreateQueuedBuild(ctx, build, []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "test", Status: domain.BuildStepStatusPending}}); buildErr != nil {
		t.Fatalf("create queued build failed: %v", buildErr)
	}
	if _, statusErr := buildRepo.UpdateStatus(ctx, build.ID, domain.BuildStatusFailed, stringPointer("pipeline-secret workspace-path")); statusErr != nil {
		t.Fatalf("mark build failed: %v", statusErr)
	}
	exitCode := 1
	if _, stepErr := buildRepo.UpdateStepByIndex(ctx, build.ID, 0, repository.StepUpdate{
		Status:       domain.BuildStepStatusFailed,
		ExitCode:     &exitCode,
		ErrorMessage: stringPointer("pipeline-secret"),
		StartedAt:    &now,
		FinishedAt:   &now,
	}); stepErr != nil {
		t.Fatalf("mark step failed: %v", stepErr)
	}

	apiTokens := service.NewAPITokenService(tokenRepo, userRepo)
	tokens := make(map[string]string)
	for _, name := range []string{"viewer", "maintainer", "owner", "outsider", "admin", "log_reader"} {
		scopes := []string{string(domain.APITokenScopeBuildRead)}
		if name == "log_reader" {
			scopes = append(scopes, string(domain.APITokenScopeBuildLogs))
		}
		created, tokenErr := apiTokens.CreateAPIToken(ctx, service.CreateAPITokenInput{UserID: users[name].ID, Name: name, Scopes: scopes})
		if tokenErr != nil {
			t.Fatalf("create %s token failed: %v", name, tokenErr)
		}
		tokens[name] = created.PlaintextToken
	}
	missingScope, tokenErr := apiTokens.CreateAPIToken(ctx, service.CreateAPITokenInput{UserID: users["missing_scope"].ID, Name: "missing scope", Scopes: []string{string(domain.APITokenScopeBuildLogs)}})
	if tokenErr != nil {
		t.Fatalf("create missing scope token failed: %v", tokenErr)
	}
	tokens["missing_scope"] = missingScope.PlaintextToken

	expiredToken := "coyote_pat_expired-token"
	expiredAt := now.Add(-time.Minute)
	if _, createExpiredErr := tokenRepo.Create(ctx, domain.APIToken{ID: "expired-token", UserID: users["viewer"].ID, Name: "expired", TokenHash: service.HashAPIToken(expiredToken), TokenPrefix: service.DisplayAPITokenPrefix(expiredToken), ExpiresAt: &expiredAt, CreatedAt: now, UpdatedAt: now}); createExpiredErr != nil {
		t.Fatalf("create expired token failed: %v", createExpiredErr)
	}
	revoked, tokenErr := apiTokens.CreateAPIToken(ctx, service.CreateAPITokenInput{UserID: users["viewer"].ID, Name: "revoked", Scopes: []string{string(domain.APITokenScopeBuildRead)}})
	if tokenErr != nil {
		t.Fatalf("create revoked token failed: %v", tokenErr)
	}
	if revokeErr := apiTokens.RevokeAPIToken(ctx, users["viewer"].ID, revoked.Token.ID); revokeErr != nil {
		t.Fatalf("revoke token failed: %v", revokeErr)
	}

	membershipService := service.NewProjectMembershipService(projectRepo, membershipRepo)
	logSink := logs.NewMemorySink()
	for _, chunk := range []logs.StepLogChunk{
		{BuildID: build.ID, StepID: "step-1", StepIndex: 0, StepName: "test", Stream: logs.StepLogStreamStdout, ChunkText: "first log\n", CreatedAt: now},
		{BuildID: build.ID, StepID: "step-1", StepIndex: 0, StepName: "test", Stream: logs.StepLogStreamStderr, ChunkText: "second log\n", CreatedAt: now},
	} {
		if _, appendErr := logSink.AppendStepLogChunk(ctx, chunk); appendErr != nil {
			t.Fatalf("append step log: %v", appendErr)
		}
	}
	buildService := buildsvc.NewBuildService(buildRepo, nil, logSink)
	reader := buildsvc.NewAuthorizedReader(buildService, authorization.NewAuthorizer(false, membershipService))
	return authenticatedMCPFixture{
		build:        build,
		tokens:       tokens,
		expiredToken: expiredToken,
		revokedToken: revoked.PlaintextToken,
		apiTokens:    apiTokens,
		buildService: buildService,
		reader:       reader,
	}
}

func (f authenticatedMCPFixture) handler(t *testing.T, disabled bool) http.Handler {
	return f.handlerWithTokens(t, f.apiTokens, disabled)
}

func (f authenticatedMCPFixture) handlerWithTokens(t *testing.T, tokens *service.APITokenService, disabled bool) http.Handler {
	t.Helper()
	reader := f.reader
	if disabled {
		reader = buildsvc.NewAuthorizedReader(
			f.buildService,
			authorization.NewAuthorizer(true, nil),
		)
	}
	handler, err := NewAuthenticatedHandler(testConfig(), nil, testImplementation(), Dependencies{
		APITokens:    tokens,
		BuildReader:  reader,
		AuthDisabled: disabled,
	})
	if err != nil {
		t.Fatalf("new authenticated handler failed: %v", err)
	}
	return handler
}

func connectMCPClient(t *testing.T, endpoint string, token string) *sdkmcp.ClientSession {
	t.Helper()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "test"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:             endpoint,
		DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: authorizationRoundTripper{
			token: token,
			base:  http.DefaultTransport,
		}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect MCP client failed: %v", err)
	}
	return session
}

type authorizationRoundTripper struct {
	token string
	base  http.RoundTripper
}

type failingGetByHashAPITokenRepository struct {
	repository.APITokenRepository
	err error
}

func (r failingGetByHashAPITokenRepository) GetByHash(context.Context, string) (domain.APIToken, error) {
	return domain.APIToken{}, r.err
}

func (t authorizationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	copyRequest := request.Clone(request.Context())
	if t.token != "" {
		copyRequest.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(copyRequest)
}

func containsTool(tools []*sdkmcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func toolResultText(result *sdkmcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	if text, ok := result.Content[0].(*sdkmcp.TextContent); ok {
		return text.Text
	}
	return ""
}

func stringPointer(value string) *string {
	return &value
}
