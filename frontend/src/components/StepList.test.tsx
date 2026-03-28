import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StepList } from './StepList';
import type { BuildStep } from '../types';

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
    render(<StepList steps={[makeStep({ command: 'echo hello' })]} />);

    const command = screen.getByTitle('echo hello');
    expect(command.textContent).toBe('echo hello');
  });

  it('truncates long command and preserves full command in title', () => {
    const longCommand = 'echo one && echo two && echo three && echo four && echo five && echo six && echo seven && echo eight';
    render(<StepList steps={[makeStep({ command: longCommand })]} />);

    const command = screen.getByTitle(longCommand);
    expect(command.textContent?.endsWith('...')).toBe(true);
    expect(command.textContent?.length).toBe(72);
  });
});
