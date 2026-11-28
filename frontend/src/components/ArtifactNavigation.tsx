import { Link, useLocation, useSearchParams } from "react-router-dom";

export function ArtifactNavigation() {
  const { pathname, search } = useLocation();
  const [params] = useSearchParams();
  const scope = new URLSearchParams();
  for (const key of ["project_id", "job_id"]) {
    const value = params.get(key);
    if (value) scope.set(key, value);
  }
  return (
    <nav aria-label="Artifact views" className="artifact-actions">
      {[
        ["/artifacts", "Build outputs"],
        ["/artifacts/logical", "Packages"],
      ].map(([path, label]) => (
        <Link
          key={path}
          aria-current={pathname === path ? "page" : undefined}
          to={{
            pathname: path,
            search: pathname === path ? search : scope.toString(),
          }}
        >
          {label}
        </Link>
      ))}
    </nav>
  );
}
