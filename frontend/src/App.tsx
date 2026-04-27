import { useEffect, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";
import { checkReadiness } from "./api/client";
import { router } from "./routes/router";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 2000,
    },
  },
});

export function App() {
  const [isReady, setIsReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let retryTimer: number | undefined;

    async function probe() {
      try {
        await checkReadiness();
        if (!cancelled) {
          setIsReady(true);
        }
      } catch {
        if (!cancelled) {
          retryTimer = window.setTimeout(probe, 1000);
        }
      }
    }

    void probe();

    return () => {
      cancelled = true;
      if (retryTimer !== undefined) {
        window.clearTimeout(retryTimer);
      }
    };
  }, []);

  if (!isReady) {
    return (
      <div className="page-shell">
        <section className="panel">
          <h2>Starting Coyote CI</h2>
          <p className="subtle-text">
            Waiting for the API and database schema to finish starting up.
          </p>
        </section>
      </div>
    );
  }

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
