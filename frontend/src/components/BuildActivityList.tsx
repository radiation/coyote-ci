import { Link } from "react-router-dom";
import { StatusBadge } from "./StatusBadge";
import type { Build, QueueEntry } from "../types/build";
import { formatTime } from "../utils/time";

type ActivityItem =
  | { kind: "queue"; entry: QueueEntry }
  | { kind: "build"; build: Build };

export function BuildActivityList({
  title,
  items,
  emptyMessage,
}: {
  title: string;
  items: ActivityItem[];
  emptyMessage: string;
}) {
  return (
    <section className="panel dashboard-panel">
      <div className="dashboard-panel-header">
        <h3>{title}</h3>
      </div>
      {items.length === 0 ? (
        <div className="empty-state">
          <p className="empty">{emptyMessage}</p>
        </div>
      ) : (
        <ul className="activity-list">
          {items.map((item) => {
            if (item.kind === "queue") {
              const entry = item.entry;
              return (
                <li
                  key={`queue-${entry.build_id}`}
                  className="activity-list-item"
                >
                  <div className="activity-list-main">
                    <div className="activity-list-title-row">
                      <Link to={`/builds/${entry.build_id}`}>
                        Build #{entry.build_number}
                      </Link>
                      <StatusBadge status={entry.status} />
                    </div>
                    <p className="subtle-text">
                      <Link to={`/projects/${entry.project_id}`}>
                        {entry.project_name?.trim() ||
                          entry.project_slug?.trim() ||
                          entry.project_id}
                      </Link>
                      {entry.job_name?.trim() ? ` · ${entry.job_name}` : ""}
                    </p>
                  </div>
                  <div className="activity-list-meta subtle-text">
                    {formatTime(
                      entry.started_at ?? entry.queued_at ?? entry.created_at,
                    )}
                  </div>
                </li>
              );
            }

            const build = item.build;
            return (
              <li key={`build-${build.id}`} className="activity-list-item">
                <div className="activity-list-main">
                  <div className="activity-list-title-row">
                    <Link to={`/builds/${build.id}`}>
                      Build{" "}
                      {build.build_number
                        ? `#${build.build_number}`
                        : build.id.slice(0, 8)}
                    </Link>
                    <StatusBadge status={build.status} />
                  </div>
                  <p className="subtle-text">
                    <Link to={`/projects/${build.project_id}`}>
                      {build.project_name?.trim() ||
                        build.project_slug?.trim() ||
                        build.project_id}
                    </Link>
                    {build.trigger_ref?.trim()
                      ? ` · ${build.trigger_ref.trim()}`
                      : ""}
                  </p>
                </div>
                <div className="activity-list-meta subtle-text">
                  {formatTime(
                    build.finished_at ??
                      build.started_at ??
                      build.queued_at ??
                      build.created_at,
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
