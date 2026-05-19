import type { ReactNode } from "react";

export function SummaryCard({
  title,
  value,
  tone = "default",
  description,
  footer,
}: {
  title: string;
  value: ReactNode;
  tone?: "default" | "info" | "warning" | "danger" | "success";
  description?: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <section className={`summary-card summary-card-${tone}`}>
      <p className="summary-card-title">{title}</p>
      <p className="summary-card-value">{value}</p>
      {description ? (
        <p className="summary-card-description">{description}</p>
      ) : null}
      {footer ? <div className="summary-card-footer">{footer}</div> : null}
    </section>
  );
}
