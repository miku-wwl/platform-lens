export function JsonPanel({ title, value, empty = "Not available for this run." }: { title: string; value: unknown; empty?: string }) {
  if (value === undefined || value === null) {
    return <section className="panel"><h3>{title}</h3><p className="muted">{empty}</p></section>;
  }
  return (
    <section className="panel">
      <h3>{title}</h3>
      <pre className="json-viewer">{typeof value === "string" ? value : JSON.stringify(value, null, 2)}</pre>
    </section>
  );
}
