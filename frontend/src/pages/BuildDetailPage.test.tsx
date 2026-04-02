import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { BuildDetailPage } from './BuildDetailPage';
import { getBuild, getBuildArtifacts, getBuildSteps, rerunBuildFromStep, retryFailedJob } from '../api';

vi.mock('../api', () => ({
  getBuild: vi.fn(),
  getBuildSteps: vi.fn(),
  getBuildArtifacts: vi.fn(),
  retryFailedJob: vi.fn(),
  rerunBuildFromStep: vi.fn(),
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/builds/build-1']}>
        <Routes>
          <Route path="/builds/:id" element={<BuildDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BuildDetailPage artifacts', () => {
  const mockedGetBuild = vi.mocked(getBuild);
  const mockedGetBuildSteps = vi.mocked(getBuildSteps);
  const mockedGetBuildArtifacts = vi.mocked(getBuildArtifacts);
  const mockedRetryFailedJob = vi.mocked(retryFailedJob);
  const mockedRerunBuildFromStep = vi.mocked(rerunBuildFromStep);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedGetBuild.mockImplementation(async (buildID: string) => {
      if (buildID === 'build-2') {
        return {
          id: 'build-2',
          project_id: 'project-1',
          status: 'queued',
          created_at: '2026-03-30T01:00:00Z',
          queued_at: '2026-03-30T01:00:01Z',
          started_at: null,
          finished_at: null,
          current_step_index: 0,
          attempt_number: 2,
          rerun_of_build_id: 'build-1',
          rerun_from_step_index: 0,
          execution_basis: 'persisted_source_and_spec',
          output_reuse_policy: 'explicit_declared_only_no_implicit_workspace',
          error_message: null,
          pipeline_source: 'repo',
          pipeline_path: 'scenarios/success-basic/coyote.yml',
          commit_sha: 'abc123def456',
        };
      }
      return {
        id: 'build-1',
        project_id: 'project-1',
        status: 'failed',
        created_at: '2026-03-30T00:00:00Z',
        queued_at: '2026-03-30T00:00:01Z',
        started_at: '2026-03-30T00:00:02Z',
        finished_at: '2026-03-30T00:00:03Z',
        current_step_index: 1,
        attempt_number: 1,
        rerun_of_build_id: null,
        rerun_from_step_index: null,
        execution_basis: 'persisted_source_and_spec',
        output_reuse_policy: 'explicit_declared_only_no_implicit_workspace',
        error_message: null,
        pipeline_source: 'repo',
        pipeline_path: 'scenarios/success-basic/coyote.yml',
        commit_sha: 'abc123def456',
      };
    });
    mockedGetBuildSteps.mockResolvedValue([
      {
        id: 'step-1',
        build_id: 'build-1',
        step_index: 0,
        name: 'verify',
        command: 'echo ok',
        status: 'failed',
        worker_id: null,
        started_at: null,
        finished_at: null,
        exit_code: 1,
        stdout: null,
        stderr: null,
        error_message: 'boom',
        job: {
          id: 'job-1',
          build_id: 'build-1',
          step_id: 'step-1',
          name: 'verify',
          step_index: 0,
          attempt_number: 1,
          retry_of_job_id: null,
          lineage_root_job_id: 'job-1',
          status: 'failed',
          image: 'golang:1.24',
          working_dir: 'backend',
          command: ['sh', '-c', 'go test ./...'],
          command_preview: 'sh -c go test ./...',
          environment: {},
          timeout_seconds: 120,
          pipeline_file_path: '.coyote/pipeline.yml',
          context_dir: '.',
          source_repo_url: 'https://github.com/acme/repo.git',
          source_commit_sha: 'abc123',
          source_ref_name: 'main',
          spec_version: 1,
          spec_digest: 'digest',
          execution_basis: 'persisted_source_and_spec',
          created_at: '2026-03-30T00:00:00Z',
          started_at: null,
          finished_at: null,
          error_message: 'boom',
          outputs: [],
        },
      },
    ]);
    mockedGetBuildArtifacts.mockResolvedValue([
      {
        id: 'artifact-1',
        build_id: 'build-1',
        path: 'dist/app',
        size_bytes: 128,
        content_type: null,
        checksum_sha256: null,
        download_url_path: '/builds/build-1/artifacts/artifact-1/download',
        created_at: '2026-03-30T00:00:04Z',
      },
    ]);
    mockedRetryFailedJob.mockResolvedValue({
      build: {
        id: 'build-2',
        project_id: 'project-1',
        status: 'queued',
        created_at: '2026-03-30T01:00:00Z',
        queued_at: '2026-03-30T01:00:01Z',
        started_at: null,
        finished_at: null,
        current_step_index: 0,
        attempt_number: 2,
        rerun_of_build_id: 'build-1',
        rerun_from_step_index: 0,
        execution_basis: 'persisted_source_and_spec',
        output_reuse_policy: 'explicit_declared_only_no_implicit_workspace',
        error_message: null,
      },
      job: {
        id: 'job-2',
        build_id: 'build-2',
        step_id: 'step-2',
        name: 'verify',
        step_index: 0,
        attempt_number: 2,
        retry_of_job_id: 'job-1',
        lineage_root_job_id: 'job-1',
        status: 'queued',
        image: 'golang:1.24',
        working_dir: 'backend',
        command: ['sh', '-c', 'go test ./...'],
        command_preview: 'sh -c go test ./...',
        environment: {},
        spec_version: 1,
        execution_basis: 'persisted_source_and_spec',
        created_at: '2026-03-30T01:00:00Z',
        outputs: [],
      },
    });
    mockedRerunBuildFromStep.mockResolvedValue({
      id: 'build-2',
      project_id: 'project-1',
      status: 'queued',
      created_at: '2026-03-30T01:00:00Z',
      queued_at: '2026-03-30T01:00:01Z',
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      attempt_number: 2,
      rerun_of_build_id: 'build-1',
      rerun_from_step_index: 0,
      execution_basis: 'persisted_source_and_spec',
      output_reuse_policy: 'explicit_declared_only_no_implicit_workspace',
      error_message: null,
    });
  });

  it('shows artifact row and download link', async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Artifacts')).toBeTruthy();
      expect(screen.getByText('dist/app')).toBeTruthy();
    });

    const link = screen.getByRole('link', { name: 'Download' });
    expect(link.getAttribute('href')).toBe('/api/builds/build-1/artifacts/artifact-1/download');
  });

  it('shows pipeline metadata when present', async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Pipeline Source')).toBeTruthy();
      expect(screen.getByText('repo')).toBeTruthy();
      expect(screen.getByText('Pipeline Path')).toBeTruthy();
      expect(screen.getByText('scenarios/success-basic/coyote.yml')).toBeTruthy();
    });
  });

  it('shows lineage and policy summary in header', async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText(/Attempt:/)).toBeTruthy();
      expect(screen.getByText('Original attempt')).toBeTruthy();
      expect(screen.getByText(/Execution Basis:/)).toBeTruthy();
      expect(screen.getByText(/Output Policy:/)).toBeTruthy();
    });
  });

  it('retries failed job and navigates to new build attempt', async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Retry failed job' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Retry failed job' }));

    await waitFor(() => {
      expect(mockedRetryFailedJob).toHaveBeenCalledWith('job-1');
      expect(mockedGetBuild).toHaveBeenCalledWith('build-2');
    });
  });

  it('reruns build from selected step and navigates to new build attempt', async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Rerun build from this step' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Rerun build from this step' }));

    await waitFor(() => {
      expect(mockedRerunBuildFromStep).toHaveBeenCalledWith('build-1', 0);
      expect(mockedGetBuild).toHaveBeenCalledWith('build-2');
    });
  });
});
