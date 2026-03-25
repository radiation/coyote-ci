import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatusBadge } from '../components/StatusBadge';

describe('StatusBadge', () => {
  it('should render status text', () => {
    render(<StatusBadge status="pending" />);
    expect(screen.getByText('Pending')).toBeTruthy();
  });

  it('should render success status', () => {
    render(<StatusBadge status="success" />);
    expect(screen.getByText('Success')).toBeTruthy();
  });

  it('should render span element', () => {
    const { container } = render(<StatusBadge status="queued" />);
    const span = container.querySelector('span');
    expect(span).toBeTruthy();
    if (span) {
      expect(span.textContent).toBe('Queued');
    }
  });
});
