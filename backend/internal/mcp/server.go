package mcp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/radiation/coyote-ci/backend/internal/service"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

const (
	HealthPath = "/healthz"
	ReadyPath  = "/readyz"
	Path       = "/mcp"
)

type ReadinessChecker interface {
	Check(context.Context) error
}

type ReadinessCheckFunc func(context.Context) error

func (f ReadinessCheckFunc) Check(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}

func NewHandler(cfg Config, readiness ReadinessChecker, implementation *sdkmcp.Implementation) (http.Handler, error) {
	protocolHandler, err := newProtocolHandler(cfg, implementation)
	if err != nil {
		return nil, err
	}
	protectedMCP, err := protectMCP(protocolHandler, cfg, func(next http.Handler) http.Handler {
		return requireAuthentication(next)
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, health)
	mux.HandleFunc(ReadyPath, ready(readiness))
	mux.Handle(Path, protectedMCP)
	return mux, nil
}

type Dependencies struct {
	APITokens      *service.APITokenService
	BuildReader    *buildsvc.AuthorizedReader
	ArtifactReader *artifactsvc.AuthorizedReader
	AuthDisabled   bool
}

func NewAuthenticatedHandler(cfg Config, readiness ReadinessChecker, implementation *sdkmcp.Implementation, dependencies Dependencies) (http.Handler, error) {
	if dependencies.BuildReader == nil {
		return nil, errors.New("MCP build reader is required")
	}
	if !dependencies.AuthDisabled && dependencies.APITokens == nil {
		return nil, errors.New("MCP API token service is required")
	}
	protocolHandler, err := newAuthenticatedProtocolHandler(cfg, implementation, dependencies.BuildReader, dependencies.ArtifactReader)
	if err != nil {
		return nil, err
	}

	authenticate := withDisabledAuthentication
	if !dependencies.AuthDisabled {
		authenticate = withPATAuthentication(dependencies.APITokens)
	}
	protectedMCP, err := protectMCP(protocolHandler, cfg, authenticate)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, health)
	mux.HandleFunc(ReadyPath, ready(readiness))
	mux.Handle(Path, protectedMCP)
	return mux, nil
}

func newProtocolHandler(cfg Config, implementation *sdkmcp.Implementation) (http.Handler, error) {
	return newAuthenticatedProtocolHandler(cfg, implementation, nil, nil)
}

func newAuthenticatedProtocolHandler(cfg Config, implementation *sdkmcp.Implementation, buildReader *buildsvc.AuthorizedReader, artifactReader *artifactsvc.AuthorizedReader) (http.Handler, error) {
	if implementation == nil {
		return nil, errors.New("MCP implementation is required")
	}

	server := sdkmcp.NewServer(implementation, nil)
	if buildReader != nil {
		registerBuildTools(server, buildReader)
	}
	if artifactReader != nil {
		registerArtifactTools(server, artifactReader)
	}
	return sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, &sdkmcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		MaxRequestBodyBytes:          cfg.MaxRequestBodyBytes,
		PropagateRequestCancellation: true,
	}), nil
}

func protectMCP(next http.Handler, cfg Config, authenticate func(http.Handler) http.Handler) (http.Handler, error) {
	protection := http.NewCrossOriginProtection()
	for _, origin := range cfg.AllowedOrigins {
		if err := protection.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
	}

	handler := authenticate(next)
	handler = requestTimeout(cfg.RequestTimeout, handler)
	handler = limitRequestBody(cfg.MaxRequestBodyBytes, handler)
	handler = requirePost(handler)
	return protection.Handler(handler), nil
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func ready(checker ReadinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checker != nil {
			if err := checker.Check(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("not ready"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}

func requirePost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestTimeout(timeout time.Duration, next http.Handler) http.Handler {
	return http.TimeoutHandler(next, timeout, "request timed out")
}

func limitRequestBody(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	})
}

func requireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "authentication is not configured", http.StatusUnauthorized)
	})
}

func NewDatabaseReadiness(db *sql.DB) ReadinessChecker {
	return ReadinessCheckFunc(func(ctx context.Context) error {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := db.PingContext(checkCtx); err != nil {
			return err
		}

		const readinessQuery = `
			SELECT
				to_regclass('public.goose_db_version') IS NOT NULL
				AND to_regclass('public.builds') IS NOT NULL
				AND to_regclass('public.build_jobs') IS NOT NULL
		`
		var ready bool
		if err := db.QueryRowContext(checkCtx, readinessQuery).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return errors.New("database schema not ready")
		}
		return nil
	})
}
