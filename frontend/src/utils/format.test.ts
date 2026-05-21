import { describe, expect, it } from "vitest";
import {
  artifactSecondaryPath,
  artifactTitle,
  artifactTypeLabel,
  formatChecksumDisplay,
  formatFileSize,
} from "./format";

describe("format helpers", () => {
  it("formats zero-byte file sizes", () => {
    expect(formatFileSize(0)).toBe("0 B");
  });

  it("prefers artifact names and omits duplicate secondary paths", () => {
    expect(
      artifactTitle({ name: " package.tgz ", path: "dist/package.tgz" }),
    ).toBe("package.tgz");
    expect(
      artifactSecondaryPath({
        name: " dist/package.tgz ",
        path: "dist/package.tgz",
      }),
    ).toBeNull();
    expect(
      artifactSecondaryPath({ name: "package.tgz", path: "dist/package.tgz" }),
    ).toBe("dist/package.tgz");
  });

  it("formats artifact type labels and checksum previews", () => {
    expect(artifactTypeLabel("docker_image")).toBe("Docker image");
    expect(formatChecksumDisplay("short-checksum")).toBe("short-checksum");
    expect(
      formatChecksumDisplay(
        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      ),
    ).toBe("0123456789ab…89abcdef");
  });
});
