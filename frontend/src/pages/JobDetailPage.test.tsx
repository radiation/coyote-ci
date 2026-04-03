import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { JobDetailPage } from './JobDetailPage';
import { getJob, runJob, updateJob } from '../api';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

vi.mock('../api', () => ({
  getJob: vi.fn(),
  updateJob: vi.fn(),
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
      <MemoryRouter initialEntries={['/jobs/job-1']}>
        <Routes>
          <Route path="/jobs/:id" element={<JobDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('JobDetailPage', () => {
  const mockedGetJob = vi.mocked(getJob);
  const mockedUpdateJob = vi.mocked(updateJob);
  const mockedRunJob = vi.mocked(runJob);

  beforeEach(() => {
    vi.clearAllMocks();

    mockedGetJob.mockResolvedValue({
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
    });

    mockedUpdateJob.mockResolvedValue({
      id: 'job-1',
      project_id: 'project-1',
      name: 'backend-ci-updated',
      repository_url: 'https://github.com/example/backend.git',
      default_ref: 'main',
      default_commit_sha: null,
      push_enabled: true,
      push_branch: 'main',
      pipeline_yaml: 'version: 1\nsteps:\n  - name: test\n    run: go test ./...\n',
      pipeline_path: '.coyote/pipeline.yml',
      enabled: true,
      created_at: '2026-03-30T00:00:00Z',
      updated_at: '2026-03-30T00:00:01Z',
    });

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

  it('loads job and saves edits', async () => {
    renderPage();

    await screen.findByDisplayValue('backend-ci');

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'backend-ci-updated' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save Job' }));

    await waitFor(() => {
      expect(mockedUpdateJob).toHaveBeenCalledWith('job-1', {
        name: 'backend-ci-updated',
        repository_url: 'https://github.com/example/backend.git',
        default_ref: 'main',
        push_enabled: true,
        push_branch: 'main',
        pipeline_yaml: 'version: 1\nsteps:\n  - name: test\n    run: go test ./...',
        pipeline_path: '.coyote/pipeline.yml',
        enabled: true,
      });
      expect(screen.getByText('Job saved.')).toBeTruthy();
    });
  });

  it('runs now and navigates to build detail', async () => {
    renderPage();

    await screen.findByDisplayValue('backend-ci');

    fireEvent.click(screen.getByRole('button', { name: 'Run Now' }));

    await waitFor(() => {
      expect(mockedRunJob).toHaveBeenCalledWith('job-1');
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-123');
    });
  });

  it('surfaces run-now error message', async () => {
    mockedRunJob.mockRejectedValueOnce(new Error('API 409: job is disabled'));

    renderPage();

    await screen.findByDisplayValue('backend-ci');

    fireEvent.click(screen.getByRole('button', { name: 'Run Now' }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to run job/)).toBeTruthy();
      expect(screen.getByText(/job is disabled/)).toBeTruthy();
    });
  });
});
