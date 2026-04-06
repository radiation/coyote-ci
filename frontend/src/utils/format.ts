const UNITS = ["B", "KB", "MB", "GB", "TB"] as const;

/** Format a byte count as a human-readable size string (e.g. "1.2 MB"). */
export function formatFileSize(bytes: number): string {
  if (bytes === 0) return "0 B";
  const i = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    UNITS.length - 1,
  );
  const value = bytes / 1024 ** i;
  return `${i === 0 ? value : value.toFixed(1)} ${UNITS[i]}`;
}
