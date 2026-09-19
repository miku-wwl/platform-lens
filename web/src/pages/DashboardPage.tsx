import { useEffect, useState } from "react";
import { getHealth, getReady, getVersion, listRuns } from "../api/client";
import type { HealthResponse, Run, VersionInfo } from "../api/types";
import { RunTable } from "../components/RunTable";
import { StatusBadge } from "../components/StatusBadge";

export function DashboardPage({ onSelectRun }: { onSelectRun: (runID: string) => void }) {
  const [health, setHealth] = useState<HealthResponse>();
  const [ready, setReady] = useState<HealthResponse>();
  const [version, setVersion] = useState<VersionInfo>();
  const [runs, setRuns] = useState<Run[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    let mounted = true;
    Promise.allSettled([getHealth(), getReady(), getVersion(), listRuns({ limit: 25 })]).then((results) => {
      if (!mounted) return;
      const [healthResult, readyResult, versionResult, runsResult] = results;
      if (healthResult.status === "fulfilled") setHealth(healthResult.value);
      if (readyResult.status === "fulfilled") setReady(readyResult.value);
      else setReady({ status: "not_ready" });
      if (versionResult.status === "fulfilled") setVersion(versionResult.value);
      if (runsResult.status === "fulfilled") setRuns(runsResult.value.runs);
      else setError("Run summary is unavailable. Check the Go backend.");
    });
    return () => { mounted = false; };
  }, []);

  const completed = runs.filter((run) => run.state === "COMPLETED").length;
  const failed = runs.filter((run) => run.state === "FAILED").length;
  const active = runs.filter((run) => !["COMPLETED", "FAILED"].includes(run.state)).length;
  const recovered = runs.filter((run) => run.attempt_no > 1 || (run.winning_attempt ?? 1) > 1).length;

  return (
    <div className="page-stack">
      <div className="page-heading"><div><p className="eyebrow">OPERATIONS / OVERVIEW</p><h1>PlatformLens Console</h1><p className="lede">Observe authoritative analysis runs, evidence, and recovery state through the Go API.</p></div></div>
      {error && <div className="alert alert-warn">{error}</div>}
      <div className="metric-grid">
        <section className="metric-card"><span>Process health</span><StatusBadge value={health?.status ?? "unavailable"} /></section>
        <section className="metric-card"><span>Dependency readiness</span><StatusBadge value={ready?.status ?? "unavailable"} /></section>
        <section className="metric-card"><span>Active runs</span><strong>{active}</strong></section>
        <section className="metric-card"><span>Completed / failed</span><strong>{completed} / {failed}</strong></section>
        <section className="metric-card"><span>Recovery indicators</span><strong>{recovered}</strong></section>
        <section className="metric-card"><span>Version</span><strong>{version?.version ?? "—"}</strong><small>{version?.git_commit ?? "commit unavailable"}</small></section>
      </div>
      <section className="panel"><div className="section-heading"><div><p className="eyebrow">RECENT RUNS</p><h2>Latest activity</h2></div><span className="muted">Bounded API result</span></div><RunTable runs={runs.slice(0, 10)} onSelect={onSelectRun} /></section>
    </div>
  );
}
