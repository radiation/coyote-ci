import { describe, expect, it } from "vitest";
import {
  buildReconcilePlan,
  groupSubscriptions,
  slackWorkspaceLink,
} from "./NotificationsPage.helpers";

describe("NotificationsPage helpers", () => {
  it("groups rules by target and scope with mixed enabled state", () => {
    const rules = groupSubscriptions([
      {
        id: "sub-1",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-02T00:00:00Z",
      },
      {
        id: "sub-2",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_succeeded",
        enabled: false,
        created_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-03T00:00:00Z",
      },
    ]);

    expect(rules).toHaveLength(1);
    expect(rules[0]?.enabledState).toBe("mixed");
    expect(rules[0]?.eventTypes).toEqual(["build_failed", "build_succeeded"]);
    expect(rules[0]?.updatedAt).toBe("2026-07-03T00:00:00Z");
  });

  it("builds a reconcile plan with duplicate cleanup before create gaps", () => {
    const [rule] = groupSubscriptions([
      {
        id: "sub-1",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-02T00:00:00Z",
      },
      {
        id: "sub-dup",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T12:00:00Z",
      },
      {
        id: "sub-2",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_succeeded",
        enabled: false,
        created_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-03T00:00:00Z",
      },
    ]);

    const plan = buildReconcilePlan(rule!, {
      targetID: "target-1",
      scopeType: "project",
      projectID: "project-1",
      jobID: "",
      events: { build_failed: true, build_succeeded: false },
      enabled: false,
    });

    expect(plan.createEvents).toEqual([]);
    expect(plan.updateEnabledRows.map((row) => row.id)).toEqual(["sub-1"]);
    expect(plan.deleteRows.map((row) => row.id).sort()).toEqual([
      "sub-2",
      "sub-dup",
    ]);
  });

  it("rejects non-http workspace links", () => {
    expect(slackWorkspaceLink("slack://workspace")).toBeNull();
    expect(slackWorkspaceLink("https://coyote.slack.com")).toEqual({
      href: "https://coyote.slack.com",
      label: "coyote.slack.com",
    });
  });
});
