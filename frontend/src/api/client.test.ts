import { beforeEach, describe, expect, it, vi } from 'vitest';
import { listBuilds, getBuild, getBuildSteps, createBuild, queueBuild } from '../api/client';

describe('API client - types', () => {
  it('should export API functions', () => {
    expect(typeof listBuilds).toBe('function');
    expect(typeof getBuild).toBe('function');
    expect(typeof getBuildSteps).toBe('function');
    expect(typeof createBuild).toBe('function');
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
});
