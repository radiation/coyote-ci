import { describe, it, expect } from 'vitest';
import { listBuilds, getBuild, getBuildSteps, createBuild, queueBuild } from '../api/client';

describe('API client - types', () => {
  it('should export API functions', () => {
    expect(typeof listBuilds).toBe('function');
    expect(typeof getBuild).toBe('function');
    expect(typeof getBuildSteps).toBe('function');
    expect(typeof createBuild).toBe('function');
    expect(typeof queueBuild).toBe('function');
  });
});
