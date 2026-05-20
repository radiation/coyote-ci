import type { ReactNode } from "react";

export function PageHeader({
  title,
  description,
  actions,
  eyebrow,
}: {
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
  eyebrow?: ReactNode;
}) {
  return (
    <div className="page-header-row">
      <div className="page-header-copy">
        {eyebrow ? <div className="page-eyebrow">{eyebrow}</div> : null}
        <h2>{title}</h2>
        {description ? <div className="subtle-text">{description}</div> : null}
      </div>
      {actions ? <div className="page-header-actions">{actions}</div> : null}
    </div>
  );
}
