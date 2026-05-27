import { describe, expect, it } from "vitest";
import {
  isActiveBuild,
  isCancelableBuild,
  isRerunnableBuild,
  isTerminalBuild,
} from "./build";

describe("build status helpers", () => {
  it.each([
    ["pending", true],
    ["queued", true],
    ["preparing", true],
    ["running", true],
    ["success", false],
    ["failed", false],
    ["canceled", false],
  ] as const)("classifies %s active=%s", (status, expected) => {
    expect(isActiveBuild(status)).toBe(expected);
  });

  it("classifies undefined status as inactive", () => {
    expect(isActiveBuild(undefined)).toBe(false);
    expect(isTerminalBuild(undefined)).toBe(false);
  });

  it.each([
    ["pending", false],
    ["queued", true],
    ["preparing", true],
    ["running", true],
    ["success", false],
    ["failed", false],
    ["canceled", false],
  ] as const)("classifies %s cancelable=%s", (status, expected) => {
    expect(isCancelableBuild(status)).toBe(expected);
  });

  it.each([
    ["pending", false],
    ["queued", false],
    ["preparing", false],
    ["running", false],
    ["success", true],
    ["failed", true],
    ["canceled", true],
  ] as const)("classifies %s terminal=%s", (status, expected) => {
    expect(isTerminalBuild(status)).toBe(expected);
  });

  it.each([
    ["pending", false],
    ["queued", false],
    ["preparing", false],
    ["running", false],
    ["success", true],
    ["failed", true],
    ["canceled", true],
  ] as const)("classifies %s rerunnable=%s", (status, expected) => {
    expect(isRerunnableBuild(status)).toBe(expected);
  });
});
