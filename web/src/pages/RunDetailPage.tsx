import { useEffect, useMemo, useState } from "react";
import { ApiError, getArtifact, getManifest, getReport, getRun, listArtifacts } from "../api/client";
import type { ArtifactListResponse, ArtifactPayload, Run } from "../api/types";
import { JsonPanel } from "../components/JsonPanel";
import { Lifecycle } from "../components/Lifecycle";
import { StatusBadge } from "../components/StatusBadge";

const terminalStates = new Set(["COMPLETED", "FAILED"]);
const preferredArtifacts = ["validation-results.json", "diagnostics.json", "discovery.json", "source-excerpts.json", "reviewer.json", "evaluation.json", "terraform-dependencies.json"];

function valueOf(payloads: Record<string, ArtifactPayload>, name: string): ArtifactPayload | undefined { return payloads[name]; }

export function RunDetailPage({ runID }: { runID: string }) {
  const [run, setRun] = useState<Run>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [artifacts, setArtifacts] = useState<ArtifactListResponse>();
  const [payloads, setPayloads] = useState<Record<string, ArtifactPayload>>({});
  const [report, setReport] = useState<string>();
  const [manifest, setManifest] = useState<ArtifactPayload>();
  const [artifactError, setArtifactError] = useState("");
  const [selectedArtifact, setSelectedArtifact] = useState("");

  useEffect(() => {
    let mounted = true;
    let timer: number | undefined;
    async function refresh() {
      try {
        const result = await getRun(runID);
        if (!mounted) return;
        setRun(result); setError(""); setLoading(false);
        if (!terminalStates.has(result.state)) timer = window.setTimeout(refresh, 3000);
      } catch (cause) {
        if (!mounted) return;
        setError(cause instanceof ApiError ? cause.message : "Run detail unavailable."); setLoading(false);
        timer = window.setTimeout(refresh, 5000);
      }
    }
    void refresh();
    return () => { mounted = false; if (timer !== undefined) window.clearTimeout(timer); };
  }, [runID]);

  useEffect(() => {
    if (!run || !terminalStates.has(run.state)) return;
    let mounted = true;
    async function loadArtifacts() {
      try {
        const catalog = await listArtifacts(runID);
        if (!mounted) return;
        setArtifacts(catalog);
        const names = new Set(preferredArtifacts);
        catalog.artifacts.filter((item) => item.name.startsWith("evidence/")).slice(0, 20).forEach((item) => names.add(item.name));
        const entries = await Promise.all([...names].filter((name) => catalog.artifacts.some((item) => item.name === name)).map(async (name) => [name, await getArtifact(runID, name)] as const));
        if (!mounted) return;
        setPayloads(Object.fromEntries(entries));
        const [reportText, manifestValue] = await Promise.all([getReport(runID), getManifest(runID)]);
        if (mounted) { setReport(reportText); setManifest(manifestValue); }
      } catch (cause) {
        if (mounted) setArtifactError(cause instanceof ApiError ? cause.message : "Artifacts are unavailable for this run.");
      }
    }
    void loadArtifacts();
    return () => { mounted = false; };
  }, [run, runID]);

  const recoveryNote = useMemo(() => {
    if (!run || run.attempt_no <= 1) return "";
    if (run.winning_attempt === run.attempt_no && run.state === "COMPLETED") return `Run completed on attempt ${run.attempt_no}, indicating at least one prior attempt. Historical reclaim details are not inferred because they are not persisted in this view.`;
    return `This is attempt ${run.attempt_no}. Prior attempt events are not inferred from the current authoritative record.`;
  }, [run]);

  if (loading) return <div className="loading-state">Loading run {runID}…</div>;
  if (error || !run) return <div className="page-stack"><div className="alert alert-bad">{error || "Run not found."}</div></div>;

  const discovery = valueOf(payloads, "discovery.json") as { kubernetes_resources?: unknown[] } | undefined;
  const evidenceNames = artifacts?.artifacts.filter((item) => item.name.startsWith("evidence/")) ?? [];

  async function openArtifact(name: string) {
    setSelectedArtifact(name);
    if (payloads[name] !== undefined || name === "report.md") return;
    if (name === "manifest.json") { setPayloads((current) => ({ ...current, [name]: manifest })); return; }
    try {
      const value = await getArtifact(runID, name);
      setPayloads((current) => ({ ...current, [name]: value }));
    } catch (cause) {
      setArtifactError(cause instanceof ApiError ? cause.message : "Artifact content is unavailable.");
    }
  }

  return (
    <div className="page-stack">
      <div className="page-heading"><div><p className="eyebrow">RUN DETAIL / AUTHORITATIVE VIEW</p><h1>{run.run_id}</h1><p className="lede">Current state and persisted presentation artifacts. Historical lifecycle events are not fabricated.</p></div><StatusBadge value={run.state} /></div>
      <section className="panel run-hero"><div><span className="muted">Current state</span><strong>{run.state}</strong></div><div><span className="muted">Attempt</span><strong>{run.attempt_no}</strong></div><div><span className="muted">Winning attempt</span><strong>{run.winning_attempt ?? "—"}</strong></div><div><span className="muted">Lease</span><strong>{run.lease_active ? "ACTIVE" : "not active"}</strong></div></section>
      {recoveryNote && <div className="alert alert-warn">{recoveryNote}</div>}
      {run.state === "FAILED" && <div className="alert alert-bad"><strong>{run.failure_code || "RUN_FAILED"}</strong>{run.failure_message && <span>{run.failure_message}</span>}</div>}
      <section className="panel"><div className="section-heading"><div><p className="eyebrow">LIFECYCLE</p><h2>Defined state machine</h2></div><span className="muted">Current state highlighted</span></div><Lifecycle state={run.state} /></section>
      <section className="detail-grid"><section className="panel"><h3>Source identity</h3><dl className="key-values"><dt>Repository</dt><dd>{run.repository_url}</dd><dt>Requested ref</dt><dd>{run.requested_ref}</dd><dt>Resolved ref</dt><dd>{run.resolved_ref || "—"}</dd><dt>Ref type</dt><dd>{run.ref_type || "—"}</dd><dt>Commit</dt><dd className="mono">{run.commit_oid || "—"}</dd><dt>Requested path</dt><dd>{run.requested_path || "—"}</dd></dl></section><section className="panel"><h3>Run timestamps</h3><dl className="key-values"><dt>Created</dt><dd>{run.created_at}</dd><dt>Updated</dt><dd>{run.updated_at}</dd><dt>Manifest hash</dt><dd className="mono">{run.manifest_hash || "—"}</dd><dt>Manifest URI</dt><dd className="mono">{run.manifest_uri || "—"}</dd></dl></section></section>
      <section className="panel"><h2>Result and AI status</h2><div className="status-grid"><div><span>Analysis outcome</span><StatusBadge value={run.analysis_outcome} /></div><div><span>Coverage</span><StatusBadge value={run.coverage_status} /></div><div><span>Review</span><StatusBadge value={run.review_status} /></div><div><span>Evaluation</span><StatusBadge value={run.evaluation_status} /></div></div><p className="field-help">UNAVAILABLE / NOT_APPLICABLE statuses describe degraded AI paths and do not turn a deterministic COMPLETED run into FAILED.</p></section>
      {artifactError && <div className="alert alert-warn">{artifactError}</div>}
      <div className="detail-grid"><JsonPanel title="Validation results" value={valueOf(payloads, "validation-results.json")} /><JsonPanel title="Diagnostics" value={valueOf(payloads, "diagnostics.json")} /></div>
      <div className="detail-grid"><JsonPanel title="AI findings / reviewer" value={valueOf(payloads, "reviewer.json")} /><JsonPanel title="Evaluation" value={valueOf(payloads, "evaluation.json")} /></div>
      <div className="detail-grid"><JsonPanel title="Evidence excerpts" value={valueOf(payloads, "source-excerpts.json")} /><JsonPanel title="Terraform provenance" value={valueOf(payloads, "terraform-dependencies.json")} /></div>
      <section className="panel"><h3>Kubernetes results</h3>{discovery?.kubernetes_resources ? <JsonPanel title="Discovered Kubernetes resources" value={discovery.kubernetes_resources} /> : <p className="muted">No Kubernetes result artifact is available for this run.</p>}</section>
      <section className="panel"><div className="section-heading"><h2>Evidence artifacts</h2><span className="muted">{evidenceNames.length} persisted evidence file(s)</span></div>{evidenceNames.length === 0 ? <p className="muted">No evidence artifacts are available.</p> : <div className="artifact-chips">{evidenceNames.map((item) => <button key={item.name} className="chip-button" onClick={() => void openArtifact(item.name)}>{item.name.replace("evidence/", "")}</button>)}</div>}{selectedArtifact && <JsonPanel title={selectedArtifact} value={valueOf(payloads, selectedArtifact)} />}</section>
      <section className="panel"><div className="section-heading"><h2>Artifacts</h2><span className="muted">Manifest allowlisted</span></div>{!artifacts || artifacts.artifacts.length === 0 ? <p className="muted">Artifacts are not available yet.</p> : <div className="table-wrap"><table><thead><tr><th>Name</th><th>Size</th><th>SHA-256</th><th>Open</th></tr></thead><tbody>{artifacts.artifacts.map((item) => <tr key={item.name}><td className="mono">{item.name}</td><td>{item.size.toLocaleString()} bytes</td><td className="mono hash-cell">{item.sha256}</td><td><button className="link-button" onClick={() => void openArtifact(item.name)}>View content</button></td></tr>)}</tbody></table></div>}</section>
      <div className="detail-grid"><section className="panel"><h2>Report</h2>{report ? <pre className="report-viewer">{report}</pre> : <p className="muted">Report unavailable.</p>}</section><JsonPanel title="Manifest" value={manifest} /></div>
    </div>
  );
}
