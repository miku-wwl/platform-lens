type StatusBadgeProps = { value?: string; tone?: "neutral" | "good" | "warn" | "bad" };

function toneFor(value?: string): StatusBadgeProps["tone"] {
  if (!value) return "neutral";
  if (["COMPLETED", "PASS", "NO_FINDINGS", "COMPLETE", "SUPPORTED"].includes(value)) return "good";
  if (["FAILED", "FAIL", "ERROR", "FINDINGS", "UNSUPPORTED"].includes(value)) return "bad";
  if (["UNAVAILABLE", "PARTIAL", "INCONCLUSIVE", "NOT_EVALUATED", "NOT_APPLICABLE"].includes(value)) return "warn";
  return "neutral";
}

export function StatusBadge({ value, tone }: StatusBadgeProps) {
  const display = value ?? "NOT AVAILABLE";
  return <span className={`status-badge status-${tone ?? toneFor(value)}`}>{display}</span>;
}
