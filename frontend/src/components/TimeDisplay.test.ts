import { describe, it, expect } from 'vitest';
import { formatTime } from '../utils/time';

describe('formatTime', () => {
  it('should format ISO string to local datetime', () => {
    const iso = '2026-03-24T12:00:00Z';
    const result = formatTime(iso);
    // Result will be locale-dependent, so just check it's not empty and not the dash
    expect(result).not.toBe('—');
    expect(result.length).toBeGreaterThan(0);
  });

  it('should return dash for null', () => {
    expect(formatTime(null)).toBe('—');
  });

  it('should return dash for undefined', () => {
    expect(formatTime(undefined)).toBe('—');
  });

  it('should handle empty string', () => {
    expect(formatTime('')).toBe('—');
  });
});
