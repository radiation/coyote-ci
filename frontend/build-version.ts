import { readFileSync } from "node:fs";

const packageMetadata = JSON.parse(
  readFileSync(new URL("./package.json", import.meta.url), "utf8"),
) as { version: string };

export const applicationVersionDefine = JSON.stringify(
  process.env.COYOTE_APPLICATION_VERSION?.trim() || packageMetadata.version,
);
