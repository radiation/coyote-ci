import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { BuildsListPage } from './BuildsListPage';
import { createBuild, createPipelineBuild, createRepoBuild, listBuilds, queueBuild } from '../api';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

vi.mock('../api', () => ({
  listBuilds: vi.fn(),
  createBuild: vi.fn(),
  createPipelineBuild: vi.fn(),
  createRepoBuild: vi.fn(),
  queueBuild: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
      mutations: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <BuildsListPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BuildsListPage queue form', () => {
  const mockedListBuilds = vi.mocked(listBuilds);
  const mockedCreateBuild = vi.mocked(createBuild);
  const mockedCreatePipelineBuild = vi.mocked(createPipelineBuild);
  const mockedCreateRepoBuild = vi.mocked(createRepoBuild);
  const mockedQueueBuild = vi.mocked(queueBuild);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedListBuilds.mockResolvedValue([]);
    mockedCreateBuild.mockResolvedValue({
      id: 'build-123',
      project_id: 'project-1',
      status: 'pending',
      created_at: '2026-03-24T00:00:00Z',
      queued_at: null,
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
    mockedQueueBuild.mockResolvedValue({
      id: 'build-123',
      project_id: 'project-1',
      status: 'queued',
      created_at: '2026-03-24T00:00:00Z',
      queued_at: '2026-03-24T00:00:01Z',
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
    mockedCreatePipelineBuild.mockResolvedValue({
      id: 'build-pipe-1',
      project_id: 'project-1',
      status: 'queued',
      created_at: '2026-03-24T00:00:00Z',
      queued_at: '2026-03-24T00:00:01Z',
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
    mockedCreateRepoBuild.mockResolvedValue({
      id: 'build-repo-1',
      project_id: 'project-1',
      status: 'queued',
      created_at: '2026-03-24T00:00:00Z',
      queued_at: '2026-03-24T00:00:01Z',
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
  });

  it('renders template dropdown with expected options', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    const select = screen.getByLabelText('Template') as HTMLSelectElement;
    expect(select.value).toBe('default');
    expect(screen.getByRole('option', { name: 'default' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'test' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'build' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'custom' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'repo build (url + ref/commit + path)' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'inline pipeline build (yaml)' })).toBeTruthy();
  });

  it('shows custom command input when template is custom', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    expect(screen.queryByLabelText('Commands')).toBeNull();

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'custom' } });

    expect(screen.getByLabelText('Commands')).toBeTruthy();
    expect(screen.getByText('One command per line. Each line becomes a step and runs via sh -c.')).toBeTruthy();
  });

  it('queues with selected template', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'build' } });
    fireEvent.click(screen.getByRole('button', { name: 'Queue Build' }));

    await waitFor(() => {
      expect(mockedCreateBuild).toHaveBeenCalledWith({ project_id: 'project-1' });
      expect(mockedQueueBuild).toHaveBeenCalledWith('build-123', 'build', undefined);
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-123');
    });
  });

  it('queues custom template with one step per command line', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'custom' } });
    fireEvent.change(screen.getByLabelText('Commands'), {
      target: { value: 'echo ok && exit 0\n\n echo fail && exit 1 ' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Queue Build' }));

    await waitFor(() => {
      expect(mockedCreateBuild).toHaveBeenCalledWith({ project_id: 'project-1' });
      expect(mockedQueueBuild).toHaveBeenCalledWith('build-123', 'custom', [
        { command: 'echo ok && exit 0' },
        { command: 'echo fail && exit 1' },
      ]);
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-123');
    });
  });

  it('renders pipeline option in template dropdown', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    expect(screen.getByRole('option', { name: 'inline pipeline build (yaml)' })).toBeTruthy();
  });

  it('shows pipeline YAML textarea when pipeline template is selected', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    expect(screen.queryByLabelText('Pipeline YAML')).toBeNull();

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'pipeline' } });

    expect(screen.getByLabelText('Pipeline YAML')).toBeTruthy();
    expect(screen.getByText('Paste a Coyote CI pipeline definition. The backend will validate it.')).toBeTruthy();
  });

  it('shows repo inputs and helper text when repo template is selected', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    expect(screen.queryByLabelText('Repository URL')).toBeNull();
    expect(screen.queryByLabelText('Ref')).toBeNull();
    expect(screen.queryByLabelText('Pipeline YAML')).toBeNull();
    expect(screen.queryByLabelText('Commands')).toBeNull();

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'repo' } });

    expect(screen.getByLabelText('Repository URL')).toBeTruthy();
    expect(screen.getByLabelText('Ref')).toBeTruthy();
    expect(screen.getByLabelText('Commit SHA')).toBeTruthy();
    expect(screen.getByDisplayValue('main')).toBeTruthy();
    expect(screen.getByText('Advanced')).toBeTruthy();
    expect(screen.getByText('Repo builds call the backend repo endpoint and load pipeline YAML from the repository path above.')).toBeTruthy();
    expect(screen.getByText('Public HTTPS repositories are the current expected path unless credentials are separately configured.')).toBeTruthy();
    expect(screen.queryByLabelText('Pipeline YAML')).toBeNull();
    expect(screen.queryByLabelText('Commands')).toBeNull();
  });

  it('submits repo build via createRepoBuild', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'repo' } });
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/org/repo.git' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create Repo Build' }));

    await waitFor(() => {
      expect(mockedCreateRepoBuild).toHaveBeenCalledWith({
        project_id: 'project-1',
        repo_url: 'https://github.com/org/repo.git',
        ref: 'main',
      });
      expect(mockedCreateBuild).not.toHaveBeenCalled();
      expect(mockedQueueBuild).not.toHaveBeenCalled();
      expect(mockedCreatePipelineBuild).not.toHaveBeenCalled();
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-repo-1');
    });
  });

  it('submits repo build with pipeline path when advanced field is set', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'repo' } });
    fireEvent.click(screen.getByText('Advanced'));
    fireEvent.change(screen.getByLabelText('Pipeline path'), {
      target: { value: '  scenarios/success-basic/coyote.yml  ' },
    });
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/org/repo.git' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create Repo Build' }));

    await waitFor(() => {
      expect(mockedCreateRepoBuild).toHaveBeenCalledWith({
        project_id: 'project-1',
        repo_url: 'https://github.com/org/repo.git',
        ref: 'main',
        pipeline_path: 'scenarios/success-basic/coyote.yml',
      });
    });
  });

  it('displays backend errors for repo builds', async () => {
    mockedCreateRepoBuild.mockRejectedValueOnce(new Error('API 400: pipeline_not_found'));

    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'repo' } });
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/org/repo.git' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create Repo Build' }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to create repo build/)).toBeTruthy();
    });
  });

  it('submits pipeline build via createPipelineBuild', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'pipeline' } });
    fireEvent.change(screen.getByLabelText('Pipeline YAML'), {
      target: { value: 'version: 1\nsteps:\n  - name: greet\n    run: echo hi\n' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create Inline Pipeline Build' }));

    await waitFor(() => {
      expect(mockedCreatePipelineBuild).toHaveBeenCalledWith({
        project_id: 'project-1',
        pipeline_yaml: 'version: 1\nsteps:\n  - name: greet\n    run: echo hi',
      });
      expect(mockedCreateBuild).not.toHaveBeenCalled();
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-pipe-1');
    });
  });

  it('shows error when pipeline YAML is empty', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'pipeline' } });
    fireEvent.change(screen.getByLabelText('Pipeline YAML'), { target: { value: '   ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create Inline Pipeline Build' }));

    expect(screen.getByText('Pipeline YAML is required.')).toBeTruthy();
    expect(mockedCreatePipelineBuild).not.toHaveBeenCalled();
  });

  it('displays backend validation errors for pipeline builds', async () => {
    mockedCreatePipelineBuild.mockRejectedValueOnce(new Error('API 400: pipeline validation failed'));

    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'pipeline' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create Inline Pipeline Build' }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to create pipeline build/)).toBeTruthy();
    });
  });

  it('submits repo build with commit SHA when provided', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'repo' } });
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/org/repo.git' },
    });
    fireEvent.change(screen.getByLabelText('Ref'), {
      target: { value: '' },
    });
    fireEvent.change(screen.getByLabelText('Commit SHA'), {
      target: { value: 'abc123def456' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create Repo Build' }));

    await waitFor(() => {
      expect(mockedCreateRepoBuild).toHaveBeenCalledWith({
        project_id: 'project-1',
        repo_url: 'https://github.com/org/repo.git',
        commit_sha: 'abc123def456',
      });
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-repo-1');
    });
  });

  it('shows error when both ref and commit SHA are empty for repo builds', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'repo' } });
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/org/repo.git' },
    });
    fireEvent.change(screen.getByLabelText('Ref'), {
      target: { value: '   ' },
    });
    fireEvent.change(screen.getByLabelText('Commit SHA'), {
      target: { value: '   ' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create Repo Build' }));

    expect(screen.getByText('Either ref or commit SHA is required.')).toBeTruthy();
    expect(mockedCreateRepoBuild).not.toHaveBeenCalled();
  });
});
