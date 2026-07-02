import { describe, expect, it } from "vitest";
import {
  formatNotificationEmail,
  normalizeCommitAuthorPreference,
  renderPreferenceUnavailableReason,
} from "./MyNotificationsPage.helpers";

describe("MyNotificationsPage helpers", () => {
  it("formats wrapped notification email values", () => {
    expect(formatNotificationEmail("<user@example.com>", "fallback")).toBe(
      "user@example.com",
    );
    expect(formatNotificationEmail("", "fallback@example.com")).toBe(
      "fallback@example.com",
    );
  });

  it("normalizes legacy commit preference responses", () => {
    const normalized = normalizeCommitAuthorPreference({
      enabled: true,
      delivery_active: true,
      target: null,
      unavailable_reason: null,
    });

    expect(normalized.email.enabled).toBe(true);
    expect(normalized.slack.enabled).toBe(false);
  });

  it("renders Slack workspace mismatch guidance", () => {
    expect(
      renderPreferenceUnavailableReason(
        { unavailable_reason: "slack_workspace_mismatch" },
        "Slack delivery",
      ),
    ).toContain("different workspace");
  });
});
