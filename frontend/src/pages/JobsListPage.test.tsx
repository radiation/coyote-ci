import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { JobsListPage } from './JobsListPage';
import { createPipelineBuild, createRepoBuild, listJobs, runJob } from '../api';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

vi.mock('../api', () => ({
  listJobs: vi.fn(),
  runJob: vi.fn(),
  createRepoBuild: vi.fn(),
  createPipelineBuild: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <JobsListPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('JobsListPage', () => {
  const mockedListJobs = vi.mocked(listJobs);
  const mockedRunJob = vi.mocked(runJob);
  const mockedCreateRepoBuild = vi.mocked(createRepoBuild);
  const mockedCreatePipelineBuild = vi.mocked(createPipelineBuild);

  beforeEach(() => {
    vi.clearAllMocks();

    mockedListJobs.mockResolvedValue([
      {
        id: 'job-1',
        project_id: 'project-1',
        name: 'backend-ci',
        repository_url: 'https://github.com/example/backend.git',
        default_ref: 'main',
        default_commit_sha: null,
        push_enabled: true,
        push_branch: 'main',
        pipeline_yaml: 'version: 1\nsteps:\n  - name: test\n    run: go test ./...\n',
        pipeline_path: '.coyote/pipeline.yml',
        enabled: true,
        created_at: '2026-03-30T00:00:00Z',
        updated_at: '2026-03-30T00:00:00Z',
      },
    ]);

    mockedRunJob.mockResolvedValue({
      id: 'build-123',
      project_id: 'project-1',
      status: 'queued',
      created_at: '2026-03-30T00:00:00Z',
      queued_at: '2026-03-30T00:00:01Z',
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
    mockedCreateRepoBuild.mockResolvedValue({
      id: 'build-repo-1',
      project_id: 'project-1',
      status: 'queued',
      created_at: '2026-03-30T00:00:00Z',
      queued_at: '2026-03-30T00:00:01Z',
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
    mockedCreatePipelineBuild.mockResolvedValue({
      id: 'build-inline-1',
      project_id: 'project-1',
      status: 'queued',
      created_at: '2026-03-30T00:00:00Z',
      queued_at: '2026-03-30T00:00:01Z',
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
  });

  it('emphasizes jobs as primary model and exposes build attempts as secondary navigation', async () => {
    renderPage();

    await screen.findByText('backend-ci');

    expect(screen.getByText('Jobs are the primary invocation model. Builds represent execution attempts and lineage.')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'View Build Attempts' }).getAttribute('href')).toBe('/builds');
    expect(screen.getByRole('link', { name: 'Create Saved Job' }).getAttribute('href')).toBe('/jobs/new');
  });

  it('shows repo-backed invocation form by default and keeps inline flow secondary', async () => {
    renderPage();

    await screen.findByText('backend-ci');

    expect(screen.getByRole('heading', { name: 'Run Job From Repo Pipeline' })).toBeTruthy();
    expect(screen.getByLabelText('Repository URL')).toBeTruthy();
    expect(screen.getByLabelText('Ref')).toBeTruthy();
    expect(screen.getByLabelText('Commit SHA')).toBeTruthy();
    expect(screen.getByLabelText('Pipeline YAML Path')).toBeTruthy();
    const advancedInlineDetails = screen.getByText('Advanced: Run Inline Pipeline').closest('details');
    expect(advancedInlineDetails).toBeTruthy();
    expect(advancedInlineDetails?.hasAttribute('open')).toBe(false);
  });

  it('runs repo invocation with pipeline path payload and navigates to build attempt', async () => {
    renderPage();

    await screen.findByText('backend-ci');

    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/org/repo.git' },
    });
    fireEvent.change(screen.getByLabelText('Ref'), {
      target: { value: 'main' },
    });
    fireEvent.change(screen.getByLabelText('Pipeline YAML Path'), {
      target: { value: 'scenarios/success-basic/coyote.yml' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Run Job From Repo Pipeline' }));

    await waitFor(() => {
      expect(mockedCreateRepoBuild).toHaveBeenCalledWith({
        project_id: 'project-1',
        repo_url: 'https://github.com/org/repo.git',
        ref: 'main',
        pipeline_path: 'scenarios/success-basic/coyote.yml',
      });
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-repo-1');
    });
  });

  it('supports commit-only repo invocation payload', async () => {
    renderPage();

    await screen.findByText('backend-ci');

    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/org/repo.git' },
    });
    fireEvent.change(screen.getByLabelText('Ref'), {
      target: { value: '' },
    });
    fireEvent.change(screen.getByLabelText('Commit SHA'), {
      target: { value: 'abc123def456' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Run Job From Repo Pipeline' }));

    await waitFor(() => {
      expect(mockedCreateRepoBuild).toHaveBeenCalledWith({
        project_id: 'project-1',
        repo_url: 'https://github.com/org/repo.git',
        commit_sha: 'abc123def456',
      });
    });
  });

  it('validates repo invocation requires ref or commit sha', async () => {
    renderPage();

    await screen.findByText('backend-ci');

    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/org/repo.git' },
    });
    fireEvent.change(screen.getByLabelText('Ref'), {
      target: { value: '   ' },
    });
    fireEvent.change(screen.getByLabelText('Commit SHA'), {
      target: { value: '   ' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Run Job From Repo Pipeline' }));

    expect(screen.getByText('Ref or Commit SHA is required.')).toBeTruthy();
    expect(mockedCreateRepoBuild).not.toHaveBeenCalled();
  });

  it('keeps inline pipeline creation as secondary advanced flow', async () => {
    renderPage();

    await screen.findByText('backend-ci');

    fireEvent.click(screen.getByText('Advanced: Run Inline Pipeline'));
    fireEvent.change(screen.getByLabelText('Inline Pipeline YAML'), {
      target: { value: 'version: 1\nsteps:\n  - name: hello\n    run: echo hello\n' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Run Inline Pipeline' }));

    await waitFor(() => {
      expect(mockedCreatePipelineBuild).toHaveBeenCalledWith({
        project_id: 'project-1',
        pipeline_yaml: 'version: 1\nsteps:\n  - name: hello\n    run: echo hello',
      });
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-inline-1');
    });
  });

  it('renders fetched jobs', async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('backend-ci')).toBeTruthy();
      expect(screen.getByText('https://github.com/example/backend.git')).toBeTruthy();
      expect(screen.getByText('main')).toBeTruthy();
      expect(screen.getAllByText('Enabled').length).toBeGreaterThan(0);
      expect(screen.getByText('On main')).toBeTruthy();
    });
  });

  it('runs job and navigates to created build', async () => {
    renderPage();

    await screen.findByText('backend-ci');
    fireEvent.click(screen.getByRole('button', { name: 'Run Now' }));

    await waitFor(() => {
      expect(mockedRunJob).toHaveBeenCalledWith('job-1');
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-123');
    });
  });

  it('shows run-now error message', async () => {
    mockedRunJob.mockRejectedValueOnce(new Error('API 409: job is disabled'));

    renderPage();

    await screen.findByText('backend-ci');
    fireEvent.click(screen.getByRole('button', { name: 'Run Now' }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to run job/)).toBeTruthy();
      expect(screen.getByText(/job is disabled/)).toBeTruthy();
    });
  });
});
