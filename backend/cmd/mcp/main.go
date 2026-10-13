package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	coyotemcp "github.com/radiation/coyote-ci/backend/internal/mcp"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	"github.com/radiation/coyote-ci/backend/internal/platform/dbopen"
	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	"github.com/radiation/coyote-ci/backend/internal/versioninfo"
)

func main() {
	appConfig := config.Load()
	mcpConfig, mcpConfigErr := coyotemcp.LoadConfig()
	if mcpConfigErr != nil {
		log.Fatalf("failed to load MCP configuration: %v", mcpConfigErr)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if runErr := run(ctx, appConfig, mcpConfig, platformdb.Open, func(server *nethttp.Server) error {
		return server.ListenAndServe()
	}); runErr != nil {
		log.Fatal(runErr)
	}
}

func run(ctx context.Context, appConfig config.Config, mcpConfig coyotemcp.Config, openDatabase func(string, platformdb.PoolConfig) (*sql.DB, error), serve func(*nethttp.Server) error) error {
	if authModeErr := validateAuthMode(auth.ParseMode(appConfig.AuthMode), mcpConfig); authModeErr != nil {
		return authModeErr
	}
	log.Printf("database config: %s", dbopen.ConfigMode(appConfig))
	databaseURL, dbPoolConfig, databaseConfigErr := dbopen.FromConfig(appConfig)
	if databaseConfigErr != nil {
		return fmt.Errorf("failed to resolve database configuration: %w", databaseConfigErr)
	}
	db, openErr := openDatabase(databaseURL, dbPoolConfig)
	if openErr != nil {
		return fmt.Errorf("failed to connect to database: %w", openErr)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("error closing database: %v", closeErr)
		}
	}()

	buildRepo := repositorypostgres.NewBuildRepository(db)
	projectRepo := repositorypostgres.NewProjectRepository(db)
	userRepo := repositorypostgres.NewUserRepository(db)
	apiTokenRepo := repositorypostgres.NewAPITokenRepository(db)
	projectMembershipRepo := repositorypostgres.NewProjectMembershipRepository(db)
	apiTokenService := service.NewAPITokenService(apiTokenRepo, userRepo)
	projectMembershipService := service.NewProjectMembershipService(projectRepo, projectMembershipRepo)
	authDisabled := auth.ParseMode(appConfig.AuthMode) == auth.ModeDisabled
	buildReader := buildsvc.NewAuthorizedReader(
		buildsvc.NewBuildService(buildRepo, nil, logs.NewPostgresSink(db)),
		authorization.NewAuthorizer(authDisabled, projectMembershipService),
	)
	handler, handlerErr := coyotemcp.NewAuthenticatedHandler(mcpConfig, coyotemcp.NewDatabaseReadiness(db), &sdkmcp.Implementation{
		Name:    "coyote-mcp",
		Version: versioninfo.Current().Version,
	}, coyotemcp.Dependencies{
		APITokens:    apiTokenService,
		BuildReader:  buildReader,
		AuthDisabled: authDisabled,
	})
	if handlerErr != nil {
		return fmt.Errorf("failed to configure MCP server: %w", handlerErr)
	}

	server := &nethttp.Server{
		Addr:              mcpConfig.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       mcpConfig.RequestTimeout,
		WriteTimeout:      mcpConfig.RequestTimeout + 5*time.Second,
		IdleTimeout:       time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), mcpConfig.ShutdownTimeout)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil && !errors.Is(shutdownErr, nethttp.ErrServerClosed) {
			log.Printf("MCP server shutdown error: %v", shutdownErr)
		}
	}()

	log.Printf("starting MCP server on %s", mcpConfig.ListenAddr)
	if serveErr := serve(server); serveErr != nil && !errors.Is(serveErr, nethttp.ErrServerClosed) {
		return fmt.Errorf("MCP server failed: %w", serveErr)
	}
	return nil
}

func validateAuthMode(mode auth.Mode, cfg coyotemcp.Config) error {
	if mode != auth.ModeDisabled {
		return nil
	}
	if !cfg.AllowDisabledAuth {
		return errors.New("MCP server refuses AUTH_MODE=disabled without MCP_ALLOW_DISABLED_AUTH=true")
	}
	host, _, splitErr := net.SplitHostPort(cfg.ListenAddr)
	if splitErr != nil {
		return fmt.Errorf("MCP server requires a valid loopback MCP_LISTEN_ADDR when AUTH_MODE=disabled: %w", splitErr)
	}
	parsedHost := net.ParseIP(host)
	if host != "localhost" && (parsedHost == nil || !parsedHost.IsLoopback()) {
		return errors.New("MCP server requires a loopback MCP_LISTEN_ADDR when AUTH_MODE=disabled")
	}
	return nil
}
