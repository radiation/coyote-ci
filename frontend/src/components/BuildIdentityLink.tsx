import { Link } from "react-router-dom";

export function BuildIdentityLink({
  id,
  number,
  search = "",
}: {
  id: string;
  number?: number | null;
  search?: string;
}) {
  return (
    <Link to={{ pathname: `/builds/${id}`, search }}>
      Build {number != null ? `#${number}` : id.slice(0, 8)}
    </Link>
  );
}
