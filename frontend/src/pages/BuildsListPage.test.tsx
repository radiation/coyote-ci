import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { BuildsListPage } from './BuildsListPage';
import { createBuild, listBuilds, queueBuild } from '../api';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

vi.mock('../api', () => ({
  listBuilds: vi.fn(),
  createBuild: vi.fn(),
  queueBuild: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
      mutations: {
        retry: false,
      },
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

describe('BuildsListPage queue form', () => {
  const mockedListBuilds = vi.mocked(listBuilds);
  const mockedCreateBuild = vi.mocked(createBuild);
  const mockedQueueBuild = vi.mocked(queueBuild);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedListBuilds.mockResolvedValue([]);
    mockedCreateBuild.mockResolvedValue({
      id: 'build-123',
      project_id: 'project-1',
      status: 'pending',
      created_at: '2026-03-24T00:00:00Z',
      queued_at: null,
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
    mockedQueueBuild.mockResolvedValue({
      id: 'build-123',
      project_id: 'project-1',
      status: 'queued',
      created_at: '2026-03-24T00:00:00Z',
      queued_at: '2026-03-24T00:00:01Z',
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
  });

  it('renders template dropdown with expected options', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    const select = screen.getByLabelText('Template') as HTMLSelectElement;
    expect(select.value).toBe('default');
    expect(screen.getByRole('option', { name: 'default' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'test' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'build' })).toBeTruthy();
  });

  it('queues with selected template', async () => {
    renderPage();

    await screen.findByText('No builds yet.');

    fireEvent.change(screen.getByLabelText('Template'), { target: { value: 'build' } });
    fireEvent.click(screen.getByRole('button', { name: 'Queue Build' }));

    await waitFor(() => {
      expect(mockedCreateBuild).toHaveBeenCalledWith({ project_id: 'project-1' });
      expect(mockedQueueBuild).toHaveBeenCalledWith('build-123', 'build');
      expect(navigateMock).toHaveBeenCalledWith('/builds/build-123');
    });
  });
});
