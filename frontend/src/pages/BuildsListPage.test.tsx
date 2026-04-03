import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { BuildsListPage } from './BuildsListPage';
import { listBuilds } from '../api';

vi.mock('../api', () => ({
  listBuilds: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <BuildsListPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BuildsListPage', () => {
  const mockedListBuilds = vi.mocked(listBuilds);

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows empty state when no builds exist', async () => {
    mockedListBuilds.mockResolvedValue([]);
    renderPage();

    await screen.findByText('No builds yet.');
    expect(screen.getByText(/Builds are created by running a/)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'job' })).toBeTruthy();
  });

  it('renders builds table with data', async () => {
    mockedListBuilds.mockResolvedValue([
      {
        id: 'aaaa-bbbb-cccc-dddd',
        project_id: 'project-1',
        status: 'success',
        created_at: '2026-03-24T00:00:00Z',
        queued_at: '2026-03-24T00:00:01Z',
        started_at: '2026-03-24T00:00:02Z',
        finished_at: '2026-03-24T00:00:10Z',
        current_step_index: 2,
        error_message: null,
      },
    ]);
    renderPage();

    await screen.findByText('aaaa-bbb…');
    expect(screen.getByText('project-1')).toBeTruthy();
  });

  it('does not render any creation form', async () => {
    mockedListBuilds.mockResolvedValue([]);
    renderPage();

    await screen.findByText('No builds yet.');
    expect(screen.queryByLabelText('Template')).toBeNull();
    expect(screen.queryByLabelText('Project ID')).toBeNull();
    expect(screen.queryByRole('button', { name: /Queue|Create/ })).toBeNull();
  });
});
