const { useState, useEffect, useCallback, useRef } = React;

interface Check {
  name: string; expected: string; actual: string;
  result: "pass" | "fail" | "skip"; latency_ms: number | null;
}
interface Probe { up: boolean; checks: Check[]; passed: number; total: number; skipped: number; }
interface Lang {
  key: string; name: string; port: number;
  managed: boolean; proc_running: boolean; pid: number | null;
  uptime_s: number | null; external: boolean; probe: Probe;
}
interface MonitorRow {
  id: string; name: string; url: string; interval_seconds: number;
  status: string; consecutive_failures: number; updated_at: string;
}
interface ResultRow {
  monitor_name: string | null; checked_at: string; success: number;
  status_code: number | null; latency_ms: number | null; error: string | null;
}
interface DbSnap {
  path: string; exists: boolean; mtime?: string; error?: string;
  monitors: MonitorRow[]; recent_results: ResultRow[];
  counts: { monitors?: number; check_results?: number };
}
interface Dash {
  generated_at: string; db: DbSnap; languages: Lang[];
}

type Page = "overview" | "java" | "go" | "python" | "typescript" | "db";
const PAGES: Page[] = ["overview", "java", "go", "python", "typescript", "db"];

function useHashPage(): Page {
  const read = (): Page => {
    const h = window.location.hash.replace(/^#\/?/, "") as Page;
    return PAGES.includes(h) ? h : "overview";
  };
  const [page, setPage] = useState<Page>(read);
  useEffect(() => {
    const onHash = () => setPage(read());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  return page;
}

function Badge(props: { cls: string; children: React.ReactNode }) {
  return <span className={"badge " + props.cls}>{props.children}</span>;
}

function fmtUptime(s: number | null): string {
  if (s == null) return "";
  if (s < 60) return s + "s";
  if (s < 3600) return Math.floor(s / 60) + "m";
  return Math.floor(s / 3600) + "h " + Math.floor((s % 3600) / 60) + "m";
}

function StatusBadges(props: { L: Lang }) {
  const L = props.L;
  return (
    <div className="badges">
      {L.probe.up ? <Badge cls="b-green">API UP</Badge> : <Badge cls="b-red">API DOWN</Badge>}
      {L.proc_running && <Badge cls="b-blue">managed pid {L.pid} · {fmtUptime(L.uptime_s)}</Badge>}
      {L.external && <Badge cls="b-amber">external process</Badge>}
      {!L.proc_running && !L.external && <Badge cls="b-grey">no process</Badge>}
      {L.probe.up && L.probe.passed === L.probe.total && <Badge cls="b-green">spec complete</Badge>}
    </div>
  );
}

function Progress(props: { L: Lang }) {
  const p = props.L.probe;
  const pct = p.total ? Math.round((p.passed / p.total) * 100) : 0;
  const barCls = !p.up ? "failed" : p.passed === p.total ? "" : "partial";
  return (
    <div>
      <div className={"progress " + barCls}><div style={{ width: pct + "%" }} /></div>
      <div className="muted small">
        {p.up ? p.passed + " / " + p.total + " spec checks passing"
              : "server not reachable — start it to run spec checks"}
      </div>
    </div>
  );
}

function ActionButtons(props: { L: Lang; onAction: (k: string, a: string) => void; busy: string | null }) {
  const L = props.L;
  const busy = props.busy === L.key;
  return (
    <div className="btnrow">
      {!L.proc_running &&
        <button className="primary" disabled={busy}
                onClick={() => props.onAction(L.key, "start")}>▶ Start</button>}
      {L.proc_running &&
        <button className="danger" disabled={busy}
                onClick={() => props.onAction(L.key, "stop")}>■ Stop</button>}
      {L.proc_running &&
        <button disabled={busy}
                onClick={() => props.onAction(L.key, "restart")}>↻ Restart</button>}
      {busy && <span className="muted small">working…</span>}
    </div>
  );
}

function CheckRow(props: { c: Check }) {
  const c = props.c;
  const icon = c.result === "pass" ? "✓" : c.result === "fail" ? "✗" : "–";
  return (
    <div className="chk">
      <span className={"icon-" + c.result} style={{ minWidth: 14, textAlign: "center" }}>{icon}</span>
      <span className="name">{c.name}</span>
      <span className="actual">{c.actual}</span>
      <span className="lat">{c.latency_ms != null ? c.latency_ms + "ms" : ""}</span>
    </div>
  );
}

function Logs(props: { langKey: string }) {
  const [logs, setLogs] = useState<string[]>([]);
  useEffect(() => {
    let alive = true;
    const pull = () => fetch("/api/servers/" + props.langKey + "/logs")
      .then(r => r.json()).then(d => { if (alive) setLogs(d.lines || []); })
      .catch(() => {});
    pull();
    const t = setInterval(pull, 2000);
    return () => { alive = false; clearInterval(t); };
  }, [props.langKey]);
  return <pre className="logs">{logs.length ? logs.join("\n") : "no managed-process output"}</pre>;
}

/* ---------------- pages ---------------- */

function LangSummaryCard(props: { L: Lang; onAction: (k: string, a: string) => void; busy: string | null }) {
  const L = props.L;
  return (
    <div className="card">
      <h2>
        <a href={"#/" + L.key} className="titlelink">{L.name}</a>
        <span className="sub">:{L.port}</span>
      </h2>
      <StatusBadges L={L} />
      <Progress L={L} />
      <ActionButtons L={L} onAction={props.onAction} busy={props.busy} />
      <div className="btnrow">
        <a href={"#/" + L.key}><button>Details →</button></a>
      </div>
    </div>
  );
}

function OverviewPage(props: { dash: Dash; onAction: (k: string, a: string) => void; busy: string | null }) {
  const db = props.dash.db;
  return (
    <div>
      <div className="grid">
        {props.dash.languages.map(L =>
          <LangSummaryCard key={L.key} L={L} onAction={props.onAction} busy={props.busy} />)}
        <div className="card">
          <h2><a href="#/db" className="titlelink">SQLite — monitor.db</a></h2>
          <div className="badges">
            {db.exists ? <Badge cls="b-green">readable</Badge> : <Badge cls="b-red">missing</Badge>}
            {db.error && <Badge cls="b-red">error</Badge>}
          </div>
          <div className="muted small">
            <div>{db.counts.monitors ?? 0} monitors · {db.counts.check_results ?? 0} check_results</div>
            {db.mtime && <div>last write {db.mtime}</div>}
            <div className="path">{db.path}</div>
          </div>
          <div className="btnrow"><a href="#/db"><button>Open tables →</button></a></div>
        </div>
      </div>
    </div>
  );
}

function LangPage(props: { L: Lang; onAction: (k: string, a: string) => void; busy: string | null }) {
  const L = props.L;
  const [showLogs, setShowLogs] = useState(false);
  return (
    <div className="card wide">
      <h2>{L.name}<span className="sub">:{L.port}</span></h2>
      <StatusBadges L={L} />
      <Progress L={L} />
      {L.probe.up
        ? <div className="checklist">{L.probe.checks.map((c, i) => <CheckRow key={i} c={c} />)}</div>
        : <div className="down-note">Start this implementation to see which spec endpoints are done.</div>}
      <ActionButtons L={L} onAction={props.onAction} busy={props.busy} />
      <div className="btnrow">
        <button onClick={() => setShowLogs(!showLogs)}>{showLogs ? "Hide logs" : "Show logs"}</button>
        <a href="#/overview"><button>← Overview</button></a>
      </div>
      {showLogs && <Logs langKey={L.key} />}
    </div>
  );
}

function MonitorsTable(props: { rows: MonitorRow[] }) {
  return (
    <div className="scroll tall"><table>
      <thead><tr>
        <th>name</th><th>url</th><th>status</th><th>fails</th><th>every</th>
        <th>timeout</th><th>threshold</th><th>updated</th><th>id</th>
      </tr></thead>
      <tbody>
        {props.rows.map(m => (
          <tr key={m.id}>
            <td>{m.name}</td>
            <td title={m.url}>{m.url}</td>
            <td className={"st-" + m.status}>{m.status}</td>
            <td>{m.consecutive_failures}</td>
            <td>{m.interval_seconds}s</td>
            <td>{m.timeout_ms}ms</td>
            <td>{m.failure_threshold}</td>
            <td>{(m.updated_at || "").replace("T", " ").slice(0, 19)}</td>
            <td className="muted" title={m.id}>{m.id.slice(0, 8)}</td>
          </tr>))}
        {!props.rows.length && <tr><td colSpan={9} className="muted">no rows</td></tr>}
      </tbody>
    </table></div>
  );
}

function ResultsTable(props: { rows: ResultRow[] }) {
  return (
    <div className="scroll tall"><table>
      <thead><tr>
        <th>monitor</th><th>checked_at</th><th>ok</th><th>code</th><th>latency</th><th>error</th>
      </tr></thead>
      <tbody>
        {props.rows.map((r, i) => (
          <tr key={i}>
            <td>{r.monitor_name || "—"}</td>
            <td>{(r.checked_at || "").replace("T", " ").slice(0, 23)}</td>
            <td className={"ok-" + r.success}>{r.success ? "✓" : "✗"}</td>
            <td>{r.status_code ?? ""}</td>
            <td>{r.latency_ms != null ? r.latency_ms + "ms" : ""}</td>
            <td title={r.error || ""}>{r.error || ""}</td>
          </tr>))}
        {!props.rows.length && <tr><td colSpan={6} className="muted">no rows</td></tr>}
      </tbody>
    </table></div>
  );
}

function DbPage(props: { db: DbSnap }) {
  const db = props.db;
  return (
    <div>
      <div className="card wide">
        <h2>monitors <span className="sub">{db.counts.monitors ?? 0} rows</span></h2>
        <MonitorsTable rows={db.monitors} />
      </div>
      <div className="card wide" style={{ marginTop: 16 }}>
        <h2>check_results <span className="sub">{db.counts.check_results ?? 0} rows — latest 50 shown</span></h2>
        <ResultsTable rows={db.recent_results} />
      </div>
      {db.error &&
        <div className="card wide" style={{ marginTop: 16, color: "var(--red)" }}>
          DB read error: {db.error}</div>}
    </div>
  );
}

/* ---------------- shell ---------------- */

function Nav(props: { page: Page; langs: Lang[] }) {
  const item = (key: Page, label: string, extra?: string) => (
    <a href={"#/" + key} className={props.page === key ? "active" : ""}>
      {label}{extra && <span className="navmeta">{extra}</span>}
    </a>
  );
  return (
    <nav>
      {item("overview", "Overview")}
      {props.langs.map(L =>
        item(L.key as Page, L.name.split(" ")[0],
          L.probe.up ? L.probe.passed + "/" + L.probe.total : "down"))}
      {item("db", "Database")}
    </nav>
  );
}

function App() {
  const [dash, setDash] = useState<Dash | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const page = useHashPage();

  const refresh = useCallback(() => {
    fetch("/api/dashboard").then(r => r.json())
      .then(d => { setDash(d); setErr(""); })
      .catch(e => setErr(String(e)));
  }, []);

  useEffect(() => {
    refresh();
    const t = window.setInterval(refresh, 3000);
    return () => clearInterval(t);
  }, [refresh]);

  const action = (key: string, act: string) => {
    setBusy(key);
    fetch("/api/servers/" + key + "/" + act, { method: "POST" })
      .then(r => r.json())
      .then(d => { if (d.detail) setErr(key + ": " + d.detail); })
      .catch(e => setErr(String(e)))
      .finally(() => { setBusy(null); refresh(); });
  };

  const langByKey = (k: Page) => dash?.languages.find(l => l.key === k);

  return (
    <div>
      <header>
        <h1>Polyglot Health Monitor</h1>
        <span className="meta">auto-refresh 3s · {dash ? "updated " + dash.generated_at : "loading…"}</span>
        {err && <span className="meta" style={{ color: "var(--red)" }}>{err}</span>}
      </header>
      {dash && <Nav page={page} langs={dash.languages} />}
      <main>
        {!dash && <div className="card">loading…</div>}
        {dash && page === "overview" &&
          <OverviewPage dash={dash} onAction={action} busy={busy} />}
        {dash && (page === "java" || page === "go" || page === "python" || page === "typescript") &&
          langByKey(page) &&
          <LangPage L={langByKey(page)!} onAction={action} busy={busy} />}
        {dash && page === "db" && <DbPage db={dash.db} />}
      </main>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
window.__mounted = true;
