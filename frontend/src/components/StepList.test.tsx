import { describe, it, expect, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { StepList } from './StepList';
import type { BuildStep } from '../types';

vi.mock('../api', () => ({
  getStepLogs: async () => ({ build_id: 'build-1', step_index: 0, after: 0, next_sequence: 0, chunks: [] }),
  buildStepLogStreamURL: () => '/logs/stream',
}));

class FakeEventSource {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  constructor(_url: string) {}
  addEventListener() {}
  close() {}
  onerror: (() => void) | null = null;
}

// @ts-expect-error test shim
globalThis.EventSource = FakeEventSource;

function makeStep(overrides: Partial<BuildStep> = {}): BuildStep {
  return {
    id: 'step-1',
    build_id: 'build-1',
    step_index: 0,
    name: 'verify',
    command: 'echo ok',
    status: 'pending',
    worker_id: null,
    started_at: null,
    finished_at: null,
    exit_code: null,
    stdout: null,
    stderr: null,
    error_message: null,
    ...overrides,
  };
}

describe('StepList', () => {
  it('renders full command when short', () => {
    render(<StepList buildID="build-1" steps={[makeStep({ command: 'echo hello' })]} />);

    const command = screen.getByTitle('echo hello');
    expect(command.textContent).toBe('echo hello');
  });

  it('truncates long command and preserves full command in title', () => {
    const longCommand = 'echo one && echo two && echo three && echo four && echo five && echo six && echo seven && echo eight';
    render(<StepList buildID="build-1" steps={[makeStep({ command: longCommand })]} />);

    const command = screen.getByTitle(longCommand);
    expect(command.textContent?.endsWith('...')).toBe(true);
    expect(command.textContent?.length).toBe(72);
  });

  it('renders linked job details and outputs when expanded', async () => {
    render(
      <StepList
        buildID="build-1"
        steps={[
          makeStep({
            job: {
              id: 'job-1',
              build_id: 'build-1',
              step_id: 'step-1',
              name: 'verify',
              step_index: 0,
              attempt_number: 2,
              retry_of_job_id: 'job-root',
              lineage_root_job_id: 'job-root',
              status: 'running',
              image: 'golang:1.24',
              working_dir: 'backend',
              command: ['sh', '-c', 'go test ./...'],
              command_preview: 'sh -c go test ./...',
              environment: { GOFLAGS: '-mod=readonly' },
              timeout_seconds: 120,
              pipeline_file_path: '.coyote/pipeline.yml',
              context_dir: '.',
              source_repo_url: 'https://github.com/acme/repo.git',
              source_commit_sha: 'abc123',
              source_ref_name: 'main',
              spec_version: 1,
              spec_digest: 'digest',
              execution_basis: 'persisted_source_and_spec',
              created_at: '2026-04-02T00:00:00Z',
              started_at: null,
              finished_at: null,
              error_message: null,
              outputs: [
                {
                  id: 'output-1',
                  job_id: 'job-1',
                  build_id: 'build-1',
                  name: 'dist',
                  kind: 'artifact',
                  declared_path: 'dist/**',
                  destination_uri: null,
                  content_type: null,
                  size_bytes: 1234,
                  digest: 'sha256:abcd',
                  status: 'declared',
                  created_at: '2026-04-02T00:00:00Z',
                },
              ],
            },
          }),
        ]}
      />, 
    );

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'View' }));
    });

    expect(await screen.findByText('Job ID')).toBeTruthy();
    expect(screen.getByText('job-1')).toBeTruthy();
    expect(screen.getByText('golang:1.24')).toBeTruthy();
    expect(screen.getAllByText('Outputs').length).toBeGreaterThan(0);
    expect(screen.getByText('dist/**')).toBeTruthy();
    expect(screen.getByText('sha256:abcd')).toBeTruthy();
    expect(screen.getByText('1234')).toBeTruthy();
  });

  it('renders lineage and output summary in row', () => {
    render(
      <StepList
        buildID="build-1"
        steps={[
          makeStep({
            status: 'failed',
            job: {
              id: 'job-1',
              build_id: 'build-1',
              step_id: 'step-1',
              name: 'verify',
              step_index: 0,
              attempt_number: 3,
              retry_of_job_id: 'job-0',
              lineage_root_job_id: 'job-0',
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
              created_at: '2026-04-02T00:00:00Z',
              started_at: null,
              finished_at: null,
              error_message: 'boom',
              outputs: [],
            },
          }),
        ]}
      />,
    );

    expect(screen.getByText('Attempt 3')).toBeTruthy();
    expect(screen.getByText('Retry of job job-0...')).toBeTruthy();
    expect(screen.getByText('0 outputs')).toBeTruthy();
  });

  it('fires retry and rerun actions with expected args', async () => {
    const onRetryFailedJob = vi.fn();
    const onRerunFromStep = vi.fn();

    render(
      <StepList
        buildID="build-1"
        canRerunFromStep
        onRetryFailedJob={onRetryFailedJob}
        onRerunFromStep={onRerunFromStep}
        steps={[
          makeStep({
            status: 'failed',
            step_index: 4,
            job: {
              id: 'job-1',
              build_id: 'build-1',
              step_id: 'step-1',
              name: 'verify',
              step_index: 4,
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
              created_at: '2026-04-02T00:00:00Z',
              started_at: null,
              finished_at: null,
              error_message: 'boom',
              outputs: [],
            },
          }),
        ]}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Retry failed job' }));
    fireEvent.click(screen.getByRole('button', { name: 'Rerun build from this step' }));

    expect(onRetryFailedJob).toHaveBeenCalledWith('job-1');
    expect(onRerunFromStep).toHaveBeenCalledWith(4);
  });
});
