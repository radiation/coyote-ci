package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	cliconfig "github.com/radiation/coyote-ci/backend/internal/cli/config"
	"github.com/radiation/coyote-ci/backend/internal/cli/credentials"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
	"github.com/radiation/coyote-ci/backend/internal/versioninfo"
)

type ConfigStore interface {
	Load() (cliconfig.File, error)
	Save(cliconfig.File) error
	Path() (string, error)
}

type Dependencies struct {
	Context      context.Context
	Stdout       io.Writer
	Stderr       io.Writer
	Stdin        io.Reader
	ConfigStore  ConfigStore
	Credentials  credentials.Store
	HTTPClient   *http.Client
	PromptSecret func(string) (string, error)
	Getenv       func(string) string
	ConfigPath   string
	Args         []string
}

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type app struct {
	stdout       io.Writer
	stderr       io.Writer
	stdin        io.Reader
	configStore  ConfigStore
	credentials  credentials.Store
	httpClient   *http.Client
	promptSecret func(string) (string, error)
	getenv       func(string) string

	flagContext string
	flagServer  string
	flagOutput  string
	flagJSON    bool
}

type resolvedTarget struct {
	ContextName string
	ServerURL   string
	OutputMode  output.Mode
	Token       string
	AuthSource  string
	Context     *cliconfig.Context
}

type selectedContext struct {
	Context cliconfig.Context
	Source  string
}

func Run(deps Dependencies) int {
	cmd := NewRootCommand(deps)
	cmd.SetArgs(deps.Args)
	executeContext := deps.Context
	if executeContext == nil {
		executeContext = context.Background()
	}
	if err := cmd.ExecuteContext(executeContext); err != nil {
		stderr := deps.Stderr
		if stderr == nil {
			stderr = os.Stderr
		}
		_, _ = fmt.Fprintln(stderr, err.Error())
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			return exitErr.Code
		}
		if errors.Is(err, context.Canceled) {
			return 130
		}
		return 1
	}
	return 0
}

func NewRootCommand(deps Dependencies) *cobra.Command {
	stdout := deps.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := deps.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdin := deps.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	configStore := deps.ConfigStore
	if configStore == nil {
		configStore = cliconfig.NewStore(deps.ConfigPath)
	}
	credentialStore := deps.Credentials
	if credentialStore == nil {
		credentialStore = credentials.NewKeyringStore("coyote-cli")
	}
	getenv := deps.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	application := &app{
		stdout:      stdout,
		stderr:      stderr,
		stdin:       stdin,
		configStore: configStore,
		credentials: credentialStore,
		httpClient:  deps.HTTPClient,
		getenv:      getenv,
	}
	application.promptSecret = deps.PromptSecret
	if application.promptSecret == nil {
		application.promptSecret = application.defaultPromptSecret
	}

	rootCmd := &cobra.Command{
		Use:           "coyote",
		Short:         "Coyote CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.PersistentFlags().StringVar(&application.flagContext, "context", "", "Context name")
	rootCmd.PersistentFlags().StringVar(&application.flagServer, "server", "", "Server URL override")
	rootCmd.PersistentFlags().StringVar(&application.flagOutput, "output", "", "Output mode: human or json")
	rootCmd.PersistentFlags().BoolVar(&application.flagJSON, "json", false, "Emit JSON output")

	rootCmd.AddCommand(application.newVersionCommand())
	rootCmd.AddCommand(application.newContextCommand())
	rootCmd.AddCommand(application.newAuthCommand())
	rootCmd.AddCommand(application.newBuildCommand())
	rootCmd.AddCommand(application.newServerCommand())

	return rootCmd
}

func (a *app) newVersionCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "version",
		Short: "Show CLI version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := a.resolveOutputMode(cliconfig.Context{}, false)
			if err != nil {
				return err
			}
			info := versioninfo.Current()
			payload := map[string]any{"cli": map[string]string{"version": info.Version, "commit": info.Commit, "build_date": info.BuildDate}}
			return output.Write(mode, a.stdout, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "coyote %s\ncommit: %s\nbuild date: %s\n", info.Version, info.Commit, valueOrUnknown(info.BuildDate))
				return err
			}, payload)
		},
	}
	return command
}

func (a *app) newContextCommand() *cobra.Command {
	command := &cobra.Command{Use: "context", Short: "Manage named server contexts"}

	var serverURL string
	var defaultOutput string
	addCommand := &cobra.Command{
		Use:   "add <name>",
		Short: "Add or update a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := cliconfig.NormalizeContextName(args[0])
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			normalizedServer, err := cliconfig.NormalizeServerURL(serverURL)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			normalizedOutput, err := cliconfig.NormalizeOutput(defaultOutput)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			cfg, err := a.configStore.Load()
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			if cfg.Contexts == nil {
				cfg.Contexts = map[string]cliconfig.Context{}
			}
			ctx := cfg.Contexts[name]
			ctx.Name = name
			ctx.ServerURL = normalizedServer
			if normalizedOutput != "" {
				ctx.DefaultOutput = normalizedOutput
			}
			if strings.TrimSpace(ctx.CredentialRef) == "" {
				ctx.CredentialRef = defaultCredentialRef(name)
			}
			cfg.Contexts[name] = ctx
			if strings.TrimSpace(cfg.CurrentContext) == "" {
				cfg.CurrentContext = name
			}
			saveErr := a.configStore.Save(cfg)
			if saveErr != nil {
				return &ExitError{Code: 3, Err: saveErr}
			}
			mode, err := a.resolveOutputMode(ctx, false)
			if err != nil {
				return err
			}
			payload := map[string]any{"context": contextJSON(ctx, cfg.CurrentContext == name)}
			return output.Write(mode, a.stdout, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Added context %s -> %s\n", name, normalizedServer)
				return err
			}, payload)
		},
	}
	addCommand.Flags().StringVar(&serverURL, "server", "", "Server URL")
	addCommand.Flags().StringVar(&defaultOutput, "default-output", "", "Default output mode: human or json")
	if err := addCommand.MarkFlagRequired("server"); err != nil {
		panic(err)
	}

	listCommand := &cobra.Command{
		Use:   "list",
		Short: "List configured contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.configStore.Load()
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			mode, err := a.resolveOutputMode(cliconfig.Context{}, false)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(cfg.Contexts))
			for name := range cfg.Contexts {
				names = append(names, name)
			}
			sort.Strings(names)
			contexts := make([]map[string]any, 0, len(names))
			for _, name := range names {
				contexts = append(contexts, contextJSON(cfg.Contexts[name], cfg.CurrentContext == name))
			}
			payload := map[string]any{"current_context": cfg.CurrentContext, "contexts": contexts}
			return output.Write(mode, a.stdout, func(w io.Writer) error {
				if len(contexts) == 0 {
					_, err := fmt.Fprintln(w, "No contexts configured")
					return err
				}
				for _, ctx := range contexts {
					marker := " "
					if current, _ := ctx["current"].(bool); current {
						marker = "*"
					}
					_, err := fmt.Fprintf(w, "%s %s -> %s\n", marker, ctx["name"], ctx["server_url"])
					if err != nil {
						return err
					}
				}
				return nil
			}, payload)
		},
	}

	useCommand := &cobra.Command{
		Use:   "use <name>",
		Short: "Select the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := cliconfig.NormalizeContextName(args[0])
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			cfg, err := a.configStore.Load()
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			ctx, ok := cfg.Contexts[name]
			if !ok {
				return &ExitError{Code: 3, Err: fmt.Errorf("unknown context %q", name)}
			}
			cfg.CurrentContext = name
			saveErr := a.configStore.Save(cfg)
			if saveErr != nil {
				return &ExitError{Code: 3, Err: saveErr}
			}
			mode, err := a.resolveOutputMode(ctx, false)
			if err != nil {
				return err
			}
			payload := map[string]any{"context": contextJSON(ctx, true)}
			return output.Write(mode, a.stdout, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Current context: %s\n", name)
				return err
			}, payload)
		},
	}

	currentCommand := &cobra.Command{
		Use:   "current",
		Short: "Show the selected context",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.configStore.Load()
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			selected, err := a.selectedContext(cfg)
			if err != nil {
				return err
			}
			mode, err := a.resolveOutputMode(selected.Context, false)
			if err != nil {
				return err
			}
			payload := map[string]any{"context": contextJSON(selected.Context, true), "source": selected.Source}
			return output.Write(mode, a.stdout, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s -> %s\n", selected.Context.Name, selected.Context.ServerURL)
				return err
			}, payload)
		},
	}

	command.AddCommand(addCommand, listCommand, useCommand, currentCommand)
	return command
}

func (a *app) newAuthCommand() *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Manage CLI authentication"}

	var readFromStdin bool
	tokenCommand := &cobra.Command{Use: "token", Short: "Manage API tokens for the CLI"}
	tokenSetCommand := &cobra.Command{
		Use:   "set",
		Short: "Store an API token for the selected context",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.configStore.Load()
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			selected, err := a.selectedContext(cfg)
			if err != nil {
				return err
			}
			ctx := selected.Context
			if strings.TrimSpace(ctx.CredentialRef) == "" {
				ctx.CredentialRef = defaultCredentialRef(ctx.Name)
				cfg.Contexts[ctx.Name] = ctx
				saveErr := a.configStore.Save(cfg)
				if saveErr != nil {
					return &ExitError{Code: 3, Err: saveErr}
				}
			}
			token, source, err := a.readTokenForSet(readFromStdin)
			if err != nil {
				return err
			}
			setErr := a.credentials.Set(ctx.CredentialRef, token)
			if setErr != nil {
				return &ExitError{Code: 3, Err: setErr}
			}
			mode, err := a.resolveOutputMode(ctx, false)
			if err != nil {
				return err
			}
			payload := map[string]any{"context": ctx.Name, "server_url": ctx.ServerURL, "auth_source": source}
			return output.Write(mode, a.stdout, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Stored token for context %s\n", ctx.Name)
				return err
			}, payload)
		},
	}
	tokenSetCommand.Flags().BoolVar(&readFromStdin, "stdin", false, "Read token from stdin")
	tokenCommand.AddCommand(tokenSetCommand)

	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			me, err := client.GetMe(cmd.Context())
			if err != nil {
				return mapCommandError(err)
			}
			payload := map[string]any{
				"context": map[string]any{
					"name":       resolved.ContextName,
					"server_url": resolved.ServerURL,
				},
				"auth": map[string]any{
					"source":        resolved.AuthSource,
					"authenticated": strings.TrimSpace(resolved.Token) != "",
					"auth_mode":     me.AuthMode,
					"auth_method":   me.AuthMethod,
				},
				"user": me.User,
			}
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Context: %s\nServer: %s\nAuth source: %s\nAuth mode: %s\nAuth method: %s\nUser: %s (%s)\n", emptyOr(resolved.ContextName, "none"), resolved.ServerURL, emptyOr(resolved.AuthSource, "none"), me.AuthMode, emptyOr(me.AuthMethod, "none"), me.User.Email, me.User.ID)
				return err
			}, payload)
		},
	}

	command.AddCommand(tokenCommand, statusCommand)
	return command
}

func (a *app) newServerCommand() *cobra.Command {
	command := &cobra.Command{Use: "server", Short: "Inspect the connected server"}
	infoCommand := &cobra.Command{
		Use:   "info",
		Short: "Show server metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			info, err := client.GetServerInfo(cmd.Context())
			if err != nil {
				return mapCommandError(err)
			}
			payload := map[string]any{"context": resolved.ContextName, "server_url": resolved.ServerURL, "server": info}
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Server: %s\nVersion: %s\nAPI version: %s\nCommit: %s\nBuild date: %s\n", resolved.ServerURL, info.Version, info.APIVersion, valueOrUnknown(info.Commit), valueOrUnknown(info.BuildDate))
				return err
			}, payload)
		},
	}
	command.AddCommand(infoCommand)
	return command
}

func (a *app) resolveTarget() (resolvedTarget, error) {
	cfg, err := a.configStore.Load()
	if err != nil {
		return resolvedTarget{}, &ExitError{Code: 3, Err: err}
	}

	var ctx *cliconfig.Context
	contextName := strings.TrimSpace(a.flagContext)
	if contextName == "" {
		contextName = strings.TrimSpace(a.getenv(cliconfig.EnvContext))
	}
	if contextName == "" {
		contextName = strings.TrimSpace(cfg.CurrentContext)
	}
	if contextName != "" {
		storedContext, ok := cfg.Contexts[contextName]
		if !ok {
			return resolvedTarget{}, &ExitError{Code: 3, Err: fmt.Errorf("unknown context %q", contextName)}
		}
		ctx = &storedContext
	}

	serverURL := strings.TrimSpace(a.flagServer)
	if serverURL == "" {
		serverURL = strings.TrimSpace(a.getenv(cliconfig.EnvServer))
	}
	if serverURL == "" && ctx != nil {
		serverURL = ctx.ServerURL
	}
	if serverURL == "" {
		return resolvedTarget{}, &ExitError{Code: 3, Err: errors.New("server url is required; set --server, COYOTE_SERVER, or select a context")}
	}
	normalizedServer, err := cliconfig.NormalizeServerURL(serverURL)
	if err != nil {
		return resolvedTarget{}, &ExitError{Code: 3, Err: err}
	}

	resolvedOutput, err := a.resolveOutputMode(derefContext(ctx), false)
	if err != nil {
		return resolvedTarget{}, err
	}

	token := strings.TrimSpace(a.getenv(cliconfig.EnvToken))
	authSource := ""
	if token != "" {
		authSource = "environment"
	} else if ctx != nil && strings.TrimSpace(ctx.CredentialRef) != "" {
		storedToken, getErr := a.credentials.Get(ctx.CredentialRef)
		if getErr != nil && !errors.Is(getErr, credentials.ErrNotFound) {
			return resolvedTarget{}, &ExitError{Code: 3, Err: getErr}
		}
		token = strings.TrimSpace(storedToken)
		if token != "" {
			authSource = "credential_store"
		}
	}

	return resolvedTarget{ContextName: contextName, ServerURL: normalizedServer, OutputMode: resolvedOutput, Token: token, AuthSource: authSource, Context: ctx}, nil
}

func (a *app) selectedContext(cfg cliconfig.File) (selectedContext, error) {
	contextName := strings.TrimSpace(a.flagContext)
	source := "flag"
	if contextName == "" {
		contextName = strings.TrimSpace(a.getenv(cliconfig.EnvContext))
		source = "environment"
	}
	if contextName == "" {
		contextName = strings.TrimSpace(cfg.CurrentContext)
		source = "config"
	}
	if contextName == "" {
		return selectedContext{}, &ExitError{Code: 3, Err: errors.New("no context is selected")}
	}
	ctx, ok := cfg.Contexts[contextName]
	if !ok {
		return selectedContext{}, &ExitError{Code: 3, Err: fmt.Errorf("unknown context %q", contextName)}
	}
	return selectedContext{Context: ctx, Source: source}, nil
}

func (a *app) readTokenForSet(forceStdin bool) (string, string, error) {
	if forceStdin {
		body, err := io.ReadAll(a.stdin)
		if err != nil {
			return "", "", &ExitError{Code: 2, Err: err}
		}
		token := strings.TrimSpace(string(body))
		if token == "" {
			return "", "", &ExitError{Code: 2, Err: errors.New("token is required")}
		}
		return token, "stdin", nil
	}
	if token := strings.TrimSpace(a.getenv(cliconfig.EnvToken)); token != "" {
		return token, "environment", nil
	}
	token, err := a.promptSecret("API token: ")
	if err != nil {
		return "", "", &ExitError{Code: 2, Err: err}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", &ExitError{Code: 2, Err: errors.New("token is required")}
	}
	return token, "prompt", nil
}

func (a *app) newClient(target resolvedTarget) (*apiclient.Client, error) {
	return apiclient.New(target.ServerURL, target.Token, versioninfo.UserAgent("coyote"), a.httpClient)
}

func (a *app) resolveOutputMode(ctx cliconfig.Context, forceJSON bool) (output.Mode, error) {
	raw := strings.TrimSpace(a.flagOutput)
	if forceJSON || a.flagJSON {
		raw = string(output.ModeJSON)
	}
	if raw == "" {
		raw = strings.TrimSpace(ctx.DefaultOutput)
	}
	mode, err := output.Normalize(raw)
	if err != nil {
		return "", &ExitError{Code: 2, Err: err}
	}
	return mode, nil
}

func (a *app) defaultPromptSecret(prompt string) (string, error) {
	if _, err := fmt.Fprint(a.stderr, prompt); err != nil {
		return "", err
	}
	if file, ok := a.stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		value, err := term.ReadPassword(int(file.Fd()))
		_, _ = fmt.Fprintln(a.stderr)
		return string(value), err
	}
	reader := bufio.NewReader(a.stdin)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return value, nil
}

func mapCommandError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &ExitError{Code: 130, Err: err}
	}
	var apiErr *apiclient.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Kind {
		case apiclient.ErrorKindAuthentication:
			return &ExitError{Code: 4, Err: err}
		case apiclient.ErrorKindAuthorization:
			return &ExitError{Code: 5, Err: err}
		case apiclient.ErrorKindNotFound:
			return &ExitError{Code: 6, Err: err}
		case apiclient.ErrorKindConflict, apiclient.ErrorKindValidation:
			return &ExitError{Code: 2, Err: err}
		case apiclient.ErrorKindTransport:
			return &ExitError{Code: 8, Err: err}
		case apiclient.ErrorKindServer:
			return &ExitError{Code: 9, Err: err}
		default:
			return &ExitError{Code: 1, Err: err}
		}
	}
	return &ExitError{Code: 1, Err: err}
}

func defaultCredentialRef(name string) string {
	return "context:" + strings.TrimSpace(name)
}

func contextJSON(ctx cliconfig.Context, current bool) map[string]any {
	return map[string]any{
		"name":           ctx.Name,
		"server_url":     ctx.ServerURL,
		"credential_ref": ctx.CredentialRef,
		"default_output": ctx.DefaultOutput,
		"current":        current,
	}
}

func emptyOr(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func valueOrUnknown(value string) string {
	return emptyOr(value, "unknown")
}

func derefContext(ctx *cliconfig.Context) cliconfig.Context {
	if ctx == nil {
		return cliconfig.Context{}
	}
	return *ctx
}
