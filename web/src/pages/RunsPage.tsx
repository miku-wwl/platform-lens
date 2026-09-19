import { useEffect, useState } from "react";
import { ApiError, listRuns } from "../api/client";
import type { Run, RunState } from "../api/types";
import { RunTable } from "../components/RunTable";

const filters: Array<RunState | ""> = ["", "QUEUED", "VALIDATING", "COMPLETED", "FAILED"];

export function RunsPage({ onSelectRun }: { onSelectRun: (runID: string) => void }) {
  const [runs, setRuns] = useState<Run[]>([]);
  const [state, setState] = useState<RunState | "">("");
  const [repository, setRepository] = useState("");
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let mounted = true;
    setLoading(true);
    listRuns({ limit: 25, offset, state: state || undefined, repository: repository.trim() || undefined }).then((result) => {
      if (!mounted) return;
      setRuns(result.runs); setHasMore(result.has_more); setError("");
    }).catch((cause) => {
      if (mounted) setError(cause instanceof ApiError ? cause.message : "Run list unavailable.");
    }).finally(() => { if (mounted) setLoading(false); });
    return () => { mounted = false; };
  }, [offset, repository, state]);

  function changeState(value: string) { setOffset(0); setState(value as RunState | ""); }
  function changeRepository(value: string) { setOffset(0); setRepository(value); }

  return (
    <div className="page-stack">
      <div className="page-heading"><div><p className="eyebrow">RUNS / AUTHORITATIVE LIST</p><h1>Runs</h1><p className="lede">The list is served by the backend repository and is bounded for operator use.</p></div></div>
      <section className="panel filters"><label>State<select value={state} onChange={(event) => changeState(event.target.value)}>{filters.map((item) => <option key={item} value={item}>{item || "All states"}</option>)}</select></label><label>Repository filter<input value={repository} onChange={(event) => changeRepository(event.target.value)} placeholder="Exact canonical URL" /></label></section>
      {error && <div className="alert alert-bad">{error}</div>}
      <section className="panel">{loading ? <div className="loading-state">Loading runs…</div> : <RunTable runs={runs} onSelect={onSelectRun} />}<div className="pagination"><button disabled={offset === 0 || loading} onClick={() => setOffset(Math.max(0, offset - 25))}>Previous</button><span>Rows {offset + 1}–{offset + runs.length}</span><button disabled={!hasMore || loading} onClick={() => setOffset(offset + 25)}>Next</button></div></section>
    </div>
  );
}
