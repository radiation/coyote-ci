package cli

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

type projectListPayload struct {
	Projects []projectView `json:"projects"`
}

type projectPayload struct {
	Project projectView `json:"project"`
}

type projectView struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	WebURL      string  `json:"web_url,omitempty"`
}

func (a *app) newProjectCommand() *cobra.Command {
	command := &cobra.Command{Use: "project", Short: "Discover projects"}
	command.AddCommand(a.newProjectListCommand())
	command.AddCommand(a.newProjectShowCommand())
	return command
}

func (a *app) newProjectListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List visible projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			response, listErr := client.ListProjects(cmd.Context())
			if listErr != nil {
				return mapCommandError(listErr)
			}

			payload := makeProjectListPayload(resolved.ServerURL, response.Projects)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeProjectListHuman(w, payload)
			}, payload)
		},
	}
}

func (a *app) newProjectShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <project-id-or-slug>",
		Short: "Show one project",
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

			project, projectErr := client.GetProject(cmd.Context(), args[0])
			if projectErr != nil {
				return mapCommandError(projectErr)
			}

			payload := makeProjectPayload(resolved.ServerURL, project)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeProjectHuman(w, payload)
			}, payload)
		},
	}
}

func makeProjectListPayload(serverURL string, projects []api.ProjectResponse) projectListPayload {
	items := make([]projectView, 0, len(projects))
	for _, project := range projects {
		items = append(items, makeProjectView(serverURL, project))
	}
	return projectListPayload{Projects: items}
}

func makeProjectPayload(serverURL string, project api.ProjectResponse) projectPayload {
	return projectPayload{Project: makeProjectView(serverURL, project)}
}

func makeProjectView(serverURL string, project api.ProjectResponse) projectView {
	return projectView{
		ID:          project.ID,
		Name:        project.Name,
		Slug:        project.Slug,
		Description: trimStringPtr(project.Description),
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
		WebURL:      resourceWebURL(serverURL, "/projects/"+url.PathEscape(strings.TrimSpace(project.ID))),
	}
}

func writeProjectListHuman(w io.Writer, payload projectListPayload) error {
	if len(payload.Projects) == 0 {
		_, err := fmt.Fprintln(w, "No projects found.")
		return err
	}

	if _, err := fmt.Fprintln(w, "Projects"); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tSLUG\tNAME"); err != nil {
		return err
	}
	for _, project := range payload.Projects {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", project.ID, project.Slug, project.Name); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeProjectHuman(w io.Writer, payload projectPayload) error {
	project := payload.Project
	if _, err := fmt.Fprintf(w, "Project: %s\n", project.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "ID:      %s\n", project.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Slug:    %s\n", project.Slug); err != nil {
		return err
	}
	if project.Description != nil {
		if _, err := fmt.Fprintf(w, "Desc:    %s\n", *project.Description); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Created: %s\n", project.CreatedAt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Updated: %s\n", project.UpdatedAt); err != nil {
		return err
	}
	if project.WebURL != "" {
		if _, err := fmt.Fprintf(w, "URL:     %s\n", project.WebURL); err != nil {
			return err
		}
	}
	return nil
}
