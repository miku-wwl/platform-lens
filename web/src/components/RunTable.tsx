import type { Run } from "../api/types";
import { StatusBadge } from "./StatusBadge";

function repositoryLabel(value: string): string {
  if (!value) return "—";
  try {
    const parsed = new URL(value);
    return `${parsed.hostname}${parsed.pathname}`.replace(/\/$/, "");
  } catch {
    const parts = value.split(/[\\/]/);
    return parts[parts.length - 1] || value;
  }
}

function formatTime(value: string): string {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

export function RunTable({ runs, onSelect }: { runs: Run[]; onSelect: (runID: string) => void }) {
  if (runs.length === 0) return <div className="empty-state">No runs match the current view.</div>;
  return (
    <div className="table-wrap">
      <table>
        <thead><tr><th>Run</th><th>Repository</th><th>State</th><th>Attempt</th><th>Outcome</th><th>Updated</th></tr></thead>
        <tbody>
          {runs.map((run) => (
            <tr key={run.run_id} onClick={() => onSelect(run.run_id)} tabIndex={0} onKeyDown={(event) => { if (event.key === "Enter") onSelect(run.run_id); }}>
              <td><button className="link-button" onClick={() => onSelect(run.run_id)}>{run.run_id.slice(0, 12)}</button><small>{run.requested_ref}</small></td>
              <td title={run.repository_url}>{repositoryLabel(run.repository_url)}</td>
              <td><StatusBadge value={run.state} /></td>
              <td>{run.attempt_no}{run.winning_attempt ? ` / win ${run.winning_attempt}` : ""}</td>
              <td><StatusBadge value={run.analysis_outcome} /></td>
              <td>{formatTime(run.updated_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
