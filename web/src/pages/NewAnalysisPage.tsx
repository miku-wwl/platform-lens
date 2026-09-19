import { FormEvent, useState } from "react";
import { ApiError, createAnalysis } from "../api/client";

export function NewAnalysisPage({ onCreated }: { onCreated: (runID: string) => void }) {
  const [repositoryURL, setRepositoryURL] = useState("");
  const [requestedRef, setRequestedRef] = useState("main");
  const [requestedPath, setRequestedPath] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    if (!repositoryURL.trim()) { setError("Repository URL is required."); return; }
    if (!requestedRef.trim()) { setError("Git reference is required."); return; }
    setSubmitting(true);
    try {
      const result = await createAnalysis({ repository_url: repositoryURL.trim(), requested_ref: requestedRef.trim(), requested_path: requestedPath.trim() || undefined });
      onCreated(result.run_id);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Analysis submission failed.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="page-stack narrow-page">
      <div className="page-heading"><div><p className="eyebrow">ANALYSIS / SUBMIT</p><h1>New Analysis</h1><p className="lede">Submit a source reference to the backend. The Go service remains authoritative for validation, evidence, and final state.</p></div></div>
      <form className="panel form-card" onSubmit={submit} noValidate>
        <label>Repository URL or approved local fixture path<input value={repositoryURL} onChange={(event) => setRepositoryURL(event.target.value)} placeholder="https://github.com/example/infrastructure.git" /></label>
        <p className="field-help">Production/default source handling accepts HTTPS. Local fixture paths require the backend TEST/DEV opt-in.</p>
        <label>Requested ref<input value={requestedRef} onChange={(event) => setRequestedRef(event.target.value)} placeholder="main" /></label>
        <label>Requested path <span className="muted">(optional)</span><input value={requestedPath} onChange={(event) => setRequestedPath(event.target.value)} placeholder="terraform/" /></label>
        {error && <div className="alert alert-bad" role="alert">{error}</div>}
        <div className="form-actions"><button className="primary-button" disabled={submitting}>{submitting ? "Submitting…" : "Submit analysis"}</button></div>
      </form>
    </div>
  );
}
