import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { JobCreatePage } from './JobCreatePage';
import { createJob } from '../api';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

vi.mock('../api', () => ({
  createJob: vi.fn(),
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
        <JobCreatePage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('JobCreatePage', () => {
  const mockedCreateJob = vi.mocked(createJob);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedCreateJob.mockResolvedValue({
      id: 'job-1',
      project_id: 'project-1',
      name: 'backend-ci',
      repository_url: 'https://github.com/example/backend.git',
      default_ref: 'main',
      push_enabled: false,
      push_branch: null,
      pipeline_yaml: 'version: 1\nsteps:\n  - name: test\n    run: go test ./...\n',
      enabled: true,
      created_at: '2026-03-30T00:00:00Z',
      updated_at: '2026-03-30T00:00:00Z',
    });
  });

  it('submits primary repo pipeline path payload and navigates to job detail', async () => {
    renderPage();

    fireEvent.change(screen.getByLabelText('Job Name'), { target: { value: ' backend-ci ' } });
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: ' https://github.com/example/backend.git ' },
    });
    fireEvent.change(screen.getByLabelText('Ref'), { target: { value: ' main ' } });
    fireEvent.change(screen.getByLabelText('Pipeline YAML Path'), {
      target: { value: ' scenarios/success-basic/coyote.yml ' },
    });

    fireEvent.click(screen.getByRole('button', { name: 'Create Job With Repo Path' }));

    await waitFor(() => {
      expect(mockedCreateJob).toHaveBeenCalledTimes(1);
      expect(mockedCreateJob.mock.calls[0][0]).toEqual({
        project_id: 'project-1',
        name: 'backend-ci',
        repository_url: 'https://github.com/example/backend.git',
        default_ref: 'main',
        pipeline_path: 'scenarios/success-basic/coyote.yml',
        push_enabled: false,
        push_branch: '',
        enabled: true,
      });
      expect(navigateMock).toHaveBeenCalledWith('/jobs/job-1');
    });
  });

  it('supports commit-only primary repo payload', async () => {
    renderPage();

    fireEvent.change(screen.getByLabelText('Job Name'), { target: { value: 'backend-ci' } });
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/example/backend.git' },
    });
    fireEvent.change(screen.getByLabelText('Ref'), { target: { value: '' } });
    fireEvent.change(screen.getByLabelText('Commit SHA'), { target: { value: ' abc123def456 ' } });

    fireEvent.click(screen.getByRole('button', { name: 'Create Job With Repo Path' }));

    await waitFor(() => {
      expect(mockedCreateJob.mock.calls[0][0]).toEqual({
        project_id: 'project-1',
        name: 'backend-ci',
        repository_url: 'https://github.com/example/backend.git',
        default_commit_sha: 'abc123def456',
        push_enabled: false,
        push_branch: '',
        enabled: true,
      });
    });
  });

  it('keeps inline yaml saved-job flow as secondary', async () => {
    renderPage();

    fireEvent.click(screen.getByText('Secondary: Create Saved Inline YAML Job'));
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: ' https://github.com/example/backend.git ' },
    });
    fireEvent.change(screen.getByLabelText('Job Name'), { target: { value: ' backend-ci ' } });
    fireEvent.change(screen.getByLabelText('Pipeline YAML'), {
      target: { value: 'version: 1\nsteps:\n  - name: test\n    run: go test ./...\n' },
    });

    fireEvent.click(screen.getByRole('button', { name: 'Create Saved Job' }));

    await waitFor(() => {
      expect(mockedCreateJob.mock.calls[0][0]).toEqual({
        project_id: 'project-1',
        name: 'backend-ci',
        repository_url: 'https://github.com/example/backend.git',
        default_ref: 'main',
        push_enabled: false,
        push_branch: '',
        pipeline_yaml: 'version: 1\nsteps:\n  - name: test\n    run: go test ./...',
        enabled: true,
      });
      expect(navigateMock).toHaveBeenCalledWith('/jobs/job-1');
    });
  });
});
