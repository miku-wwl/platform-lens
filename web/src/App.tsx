import { useEffect, useState } from "react";
import { DashboardPage } from "./pages/DashboardPage";
import { NewAnalysisPage } from "./pages/NewAnalysisPage";
import { RunDetailPage } from "./pages/RunDetailPage";
import { RunsPage } from "./pages/RunsPage";

type Route = { page: "dashboard" | "new" | "runs" | "detail"; runID?: string };

function readRoute(): Route {
  const hash = window.location.hash.replace(/^#\/?/, "");
  if (hash === "new") return { page: "new" };
  if (hash === "runs") return { page: "runs" };
  const detail = hash.match(/^runs\/([^/]+)$/);
  if (detail) return { page: "detail", runID: decodeURIComponent(detail[1]) };
  return { page: "dashboard" };
}

export default function App() {
  const [route, setRoute] = useState<Route>(readRoute);
  useEffect(() => { const update = () => setRoute(readRoute()); window.addEventListener("hashchange", update); return () => window.removeEventListener("hashchange", update); }, []);
  function navigate(path: string) { window.location.hash = path; }

  return (
    <div className="app-shell">
      <aside className="sidebar"><div className="brand"><span className="brand-mark">PL</span><div><strong>PlatformLens</strong><small>operator console</small></div></div><nav aria-label="Primary navigation"><button className={route.page === "dashboard" ? "nav-item active" : "nav-item"} onClick={() => navigate("")}>Dashboard</button><button className={route.page === "new" ? "nav-item active" : "nav-item"} onClick={() => navigate("new")}>New Analysis</button><button className={route.page === "runs" || route.page === "detail" ? "nav-item active" : "nav-item"} onClick={() => navigate("runs")}>Runs</button></nav><div className="sidebar-footer"><span className="pulse-dot" /> Go API presentation layer</div></aside>
      <main className="main-content">
        {route.page === "dashboard" && <DashboardPage onSelectRun={(id) => navigate(`runs/${encodeURIComponent(id)}`)} />}
        {route.page === "new" && <NewAnalysisPage onCreated={(id) => navigate(`runs/${encodeURIComponent(id)}`)} />}
        {route.page === "runs" && <RunsPage onSelectRun={(id) => navigate(`runs/${encodeURIComponent(id)}`)} />}
        {route.page === "detail" && route.runID && <RunDetailPage runID={route.runID} />}
      </main>
    </div>
  );
}
