import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { JobsListPage } from './JobsListPage';
import { listJobs, runJob } from '../api';

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

  beforeEach(() => {
    vi.clearAllMocks();

    mockedListJobs.mockResolvedValue([
      {
        id: 'job-1',
        project_id: 'project-1',
        name: 'backend-ci',
        repository_url: 'https://github.com/example/backend.git',
        default_ref: 'main',
        push_enabled: true,
        push_branch: 'main',
        pipeline_yaml: 'version: 1\nsteps:\n  - name: test\n    run: go test ./...\n',
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
