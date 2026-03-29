import { beforeEach, describe, expect, it, vi } from 'vitest';
import { listBuilds, getBuild, getBuildSteps, createBuild, createPipelineBuild, createRepoBuild, queueBuild } from '../api/client';

describe('API client - types', () => {
  it('should export API functions', () => {
    expect(typeof listBuilds).toBe('function');
    expect(typeof getBuild).toBe('function');
    expect(typeof getBuildSteps).toBe('function');
    expect(typeof createBuild).toBe('function');
    expect(typeof createPipelineBuild).toBe('function');
    expect(typeof createRepoBuild).toBe('function');
    expect(typeof queueBuild).toBe('function');
  });

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('sends template in queue request body when provided', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          id: 'build-1',
          project_id: 'project-1',
          status: 'queued',
          created_at: '2026-03-24T00:00:00Z',
          queued_at: '2026-03-24T00:00:01Z',
          started_at: null,
          finished_at: null,
          current_step_index: 0,
          error_message: null,
        },
      }),
    } as Response);

    await queueBuild('build-1', 'test');

    expect(fetchMock).toHaveBeenCalledWith('/api/builds/build-1/queue', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ template: 'test' }),
    });
  });

  it('sends queue request without body when template is omitted', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          id: 'build-1',
          project_id: 'project-1',
          status: 'queued',
          created_at: '2026-03-24T00:00:00Z',
          queued_at: '2026-03-24T00:00:01Z',
          started_at: null,
          finished_at: null,
          current_step_index: 0,
          error_message: null,
        },
      }),
    } as Response);

    await queueBuild('build-1');

    expect(fetchMock).toHaveBeenCalledWith('/api/builds/build-1/queue', {
      method: 'POST',
    });
  });

  it('sends queue request with custom steps when provided', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          id: 'build-1',
          project_id: 'project-1',
          status: 'queued',
          created_at: '2026-03-24T00:00:00Z',
          queued_at: '2026-03-24T00:00:01Z',
          started_at: null,
          finished_at: null,
          current_step_index: 0,
          error_message: null,
        },
      }),
    } as Response);

    await queueBuild('build-1', 'custom', [
      { command: 'echo ok && exit 0' },
      { name: 'fail', command: 'echo fail && exit 1' },
    ]);

    expect(fetchMock).toHaveBeenCalledWith('/api/builds/build-1/queue', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        template: 'custom',
        steps: [
          { command: 'echo ok && exit 0' },
          { name: 'fail', command: 'echo fail && exit 1' },
        ],
      }),
    });
  });

  it('sends pipeline YAML to /builds/pipeline', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          id: 'build-pipe-1',
          project_id: 'project-1',
          status: 'queued',
          created_at: '2026-03-24T00:00:00Z',
          queued_at: '2026-03-24T00:00:01Z',
          started_at: null,
          finished_at: null,
          current_step_index: 0,
          error_message: null,
        },
      }),
    } as Response);

    const result = await createPipelineBuild({
      project_id: 'project-1',
      pipeline_yaml: 'version: 1\nsteps:\n  - name: greet\n    run: echo hi\n',
    });

    expect(result.id).toBe('build-pipe-1');
    expect(fetchMock).toHaveBeenCalledWith('/api/builds/pipeline', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        project_id: 'project-1',
        pipeline_yaml: 'version: 1\nsteps:\n  - name: greet\n    run: echo hi\n',
      }),
    });
  });

  it('sends repo payload to /builds/repo', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          id: 'build-repo-1',
          project_id: 'project-1',
          status: 'queued',
          created_at: '2026-03-24T00:00:00Z',
          queued_at: '2026-03-24T00:00:01Z',
          started_at: null,
          finished_at: null,
          current_step_index: 0,
          error_message: null,
        },
      }),
    } as Response);

    const result = await createRepoBuild({
      project_id: 'project-1',
      repo_url: 'https://github.com/org/repo.git',
      ref: 'main',
    });

    expect(result.id).toBe('build-repo-1');
    expect(fetchMock).toHaveBeenCalledWith('/api/builds/repo', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        project_id: 'project-1',
        repo_url: 'https://github.com/org/repo.git',
        ref: 'main',
      }),
    });
  });
});
