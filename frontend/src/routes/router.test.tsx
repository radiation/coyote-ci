import { describe, expect, it } from 'vitest';
import { router } from './router';

describe('router', () => {
  it('redirects default route to jobs', () => {
    const root = router.routes[0] as { children?: Array<{ path?: string; element?: { props?: Record<string, unknown> } }> };
    const defaultRoute = root.children?.find((route) => route.path === '/');

    expect(defaultRoute).toBeTruthy();
    expect(defaultRoute?.element?.props?.to).toBe('/jobs');
  });
});
