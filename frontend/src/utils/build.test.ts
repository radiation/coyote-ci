import { describe, expect, it } from "vitest";
import { isActiveBuild, isCancelableBuild } from "./build";

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
});
