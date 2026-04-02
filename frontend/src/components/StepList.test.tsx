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
                  size_bytes: null,
                  digest: null,
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
    expect(screen.getByText('Outputs')).toBeTruthy();
    expect(screen.getByText('dist/**')).toBeTruthy();
  });
});
