import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Layout } from './Layout';

describe('Layout', () => {
  it('shows jobs-first navigation and build attempts secondary link', () => {
    render(
      <MemoryRouter>
        <Layout />
      </MemoryRouter>,
    );

    expect(screen.getByRole('link', { name: 'Jobs' }).getAttribute('href')).toBe('/jobs');
    expect(screen.getByRole('link', { name: 'Build Attempts' }).getAttribute('href')).toBe('/builds');
  });
});
