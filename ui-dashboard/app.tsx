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
  generated_at: string; db: DbSnap; languages: Lang[]; targets: Target[];
}

interface ChaosMeta {
  instance: string; region: string; mode: string;
  uptime_seconds: number; requests_seen: number; paused: boolean;
  stats?: { by_endpoint: Record<string, number>; by_code: Record<string, number> };
  config?: Record<string, number>;
}

interface ScenarioInfo {
  running: boolean; name: string; step: number; total: number;
}

interface ChaosEvent { ts: string; kind: string; detail: string }

interface Target {
  key: string; name: string; port: number;
  managed: boolean; proc_running: boolean; pid?: number | null;
  uptime_s?: number | null; external: boolean; up: boolean;
  meta: ChaosMeta | null;
  scenario: ScenarioInfo | null;
  available_scenarios: Record<string, { desc: string }> | null;
  history: ChaosEvent[];
}

interface MkPod {
  name: string; phase: string; ready: boolean; restarts: number;
  status: string; fleet: string; mode?: string;
}
interface MkFleet {
  namespace: string; nodeport: number; url: string | null;
  up: boolean; meta: ChaosMeta | null; pods: MkPod[]; ready_pods: number;
}
interface MinikubeInfo {
  installed: boolean; running: boolean; ip: string | null;
  fleets: MkFleet[];
}
interface DockerTarget {
  name: string; port: number; url: string; up: boolean; meta: ChaosMeta | null;
}
interface Fixtures {
  docker: DockerTarget[];
  minikube: MinikubeInfo;
}

type Page = "overview" | "java" | "go" | "python" | "typescript" | "chaos" | "db";
const PAGES: Page[] = ["overview", "java", "go", "python", "typescript", "chaos", "db"];
const MODES = ["healthy", "jitter", "degraded", "slow", "flapping", "reset", "down"];

function modeBadge(mode: string | undefined): string {
  switch (mode) {
    case "healthy": return "b-green";
    case "down": case "reset": return "b-red";
    case "degraded": case "flapping": return "b-amber";
    default: return "b-blue";
  }
}

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

function TargetSummaryCard(props: { T: Target; onAction: (k: string, a: string) => void; busy: string | null }) {
  const T = props.T;
  return (
    <div className="card">
      <h2>
        <a href="#/chaos" className="titlelink">{T.name}</a>
        <span className="sub">:{T.port}</span>
      </h2>
      <div className="badges">
        {T.up ? <Badge cls="b-green">TARGET UP</Badge> : <Badge cls="b-red">TARGET DOWN</Badge>}
        {T.meta && <Badge cls={modeBadge(T.meta.mode)}>mode: {T.meta.mode}</Badge>}
        {T.meta?.paused && <Badge cls="b-amber">paused</Badge>}
        {T.scenario && <Badge cls="b-blue">scenario {T.scenario.name} {T.scenario.step}/{T.scenario.total}</Badge>}
        {T.proc_running && <Badge cls="b-blue">managed pid {T.pid} · {fmtUptime(T.uptime_s ?? null)}</Badge>}
        {T.external && <Badge cls="b-amber">external process</Badge>}
        {!T.proc_running && !T.external && <Badge cls="b-grey">no process</Badge>}
      </div>
      <div className="muted small">
        {T.meta
          ? `${T.meta.requests_seen} polls · instance ${T.meta.instance} · up ${fmtUptime(T.meta.uptime_seconds)}`
          : "start it, then break it — the monitors will notice"}
      </div>
      <ActionButtons L={{ key: T.key, proc_running: T.proc_running } as Lang} onAction={props.onAction} busy={props.busy} />
      <div className="btnrow">
        <a href="#/chaos"><button>Chaos controls →</button></a>
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
        {(props.dash.targets || []).map(T =>
          <TargetSummaryCard key={T.key} T={T} onAction={props.onAction} busy={props.busy} />)}
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

function ChaosPage(props: {
  T: Target; langs: Lang[];
  onAction: (k: string, a: string) => void;
  onChaos: (p: Record<string, unknown>) => void;
  busy: string | null;
}) {
  const T = props.T;
  const meta = T.meta;
  const [watchMsg, setWatchMsg] = useState("");
  const [showLogs, setShowLogs] = useState(false);

  const watch = (l: Lang) => {
    setWatchMsg("registering…");
    fetch(`/api/targets/${T.key}/watch/${l.key}`, { method: "POST" })
      .then(r => r.json())
      .then(d => setWatchMsg(d.ok
        ? `monitor '${d.monitor?.name}' → ${d.monitor?.url} created on ${l.name}`
        : (d.detail || `failed: HTTP ${d.status}`)))
      .catch(e => setWatchMsg(String(e)));
  };

  const stats = meta?.stats;
  const hist = [...T.history].reverse();

  return (
    <div>
      <div className="card wide">
        <h2>{T.name}<span className="sub">:{T.port} — the upstream your monitors poll</span></h2>
        <div className="badges">
          {T.up ? <Badge cls="b-green">TARGET UP</Badge> : <Badge cls="b-red">TARGET DOWN</Badge>}
          {meta && <Badge cls={modeBadge(meta.mode)}>mode: {meta.mode}</Badge>}
          {meta?.paused && <Badge cls="b-amber">paused</Badge>}
          {T.scenario && <Badge cls="b-blue">scenario {T.scenario.name} step {T.scenario.step}/{T.scenario.total}</Badge>}
          {T.proc_running && <Badge cls="b-blue">managed pid {T.pid} · {fmtUptime(T.uptime_s ?? null)}</Badge>}
          {T.external && <Badge cls="b-amber">external process</Badge>}
        </div>
        {meta &&
          <div className="muted small">
            instance {meta.instance} · region {meta.region} · {meta.requests_seen} polls · up {fmtUptime(meta.uptime_seconds)}
          </div>}
        <ActionButtons L={{ key: T.key, proc_running: T.proc_running } as Lang} onAction={props.onAction} busy={props.busy} />

        {T.up
          ? <div>
              <h3 style={{ marginTop: 14 }}>Mode — one click turns the whole target bad</h3>
              <div className="btnrow" style={{ flexWrap: "wrap" }}>
                {MODES.map(m =>
                  <button key={m} className={m === meta?.mode ? "primary" : ""}
                          onClick={() => props.onChaos({ action: "mode", mode: m })}>{m}</button>)}
              </div>
              <div className="btnrow">
                <button onClick={() => props.onChaos({ action: "pause", seconds: 10 })}>Pause 10s</button>
                <button onClick={() => props.onChaos({ action: "pause", seconds: 30 })}>Pause 30s</button>
                <button onClick={() => props.onChaos({ action: "reset" })}>Reset → healthy</button>
                <button className="danger"
                        onClick={() => props.onChaos({ action: "crash" })}>Crash (exit 1)</button>
              </div>

              <h3 style={{ marginTop: 14 }}>Scenarios — canned timed sequences</h3>
              <div className="btnrow" style={{ flexWrap: "wrap" }}>
                {Object.entries(T.available_scenarios || {}).map(([name, def]) =>
                  <button key={name} title={def.desc}
                          onClick={() => props.onChaos({ action: "scenario", name })}>{name}</button>)}
                {T.scenario &&
                  <button className="danger"
                          onClick={() => props.onChaos({ action: "scenario-stop" })}>
                    Cancel {T.scenario.name}</button>}
              </div>
              {T.scenario &&
                <div className="muted small">running '{T.scenario.name}' — step {T.scenario.step} of {T.scenario.total}</div>}

              <h3 style={{ marginTop: 14 }}>Watch — register this target with a monitor impl</h3>
              <div className="btnrow" style={{ flexWrap: "wrap" }}>
                {props.langs.map(l =>
                  <button key={l.key} disabled={!l.probe.up}
                          title={l.probe.up ? "" : "start the impl first"}
                          onClick={() => watch(l)}>Watch via {l.name.split(" ")[0]}</button>)}
              </div>
              {watchMsg && <div className="muted small">{watchMsg}</div>}
              <div className="muted small">then open <a className="titlelink" href="#/db">Database</a> and flip a mode — the monitor row goes DOWN.</div>
            </div>
          : <div className="down-note">Target unreachable — hit Start above, or use the minikube fleets below.</div>}

        <div className="btnrow">
          <button onClick={() => setShowLogs(!showLogs)}>{showLogs ? "Hide logs" : "Show logs"}</button>
          <a href="#/overview"><button>← Overview</button></a>
        </div>
        {showLogs && <Logs langKey={T.key} />}
      </div>

      {meta &&
        <div className="dbwrap">
          <div className="card">
            <h3>poll stats — what the target actually served</h3>
            {stats &&
              <div className="scroll">
                <table>
                  <thead><tr><th>endpoint</th><th>count</th></tr></thead>
                  <tbody>
                    {Object.entries(stats.by_endpoint).map(([ep, n]) =>
                      <tr key={ep}><td>{ep}</td><td>{n}</td></tr>)}
                  </tbody>
                </table>
                <table style={{ marginTop: 8 }}>
                  <thead><tr><th>code</th><th>count</th></tr></thead>
                  <tbody>
                    {Object.entries(stats.by_code).map(([code, n]) =>
                      <tr key={code}>
                        <td className={code === "fault" ? "st-DOWN" : code.startsWith("2") ? "st-UP" : "st-UNKNOWN"}>{code}</td>
                        <td>{n}</td>
                      </tr>)}
                  </tbody>
                </table>
              </div>}
            {meta.config &&
              <div className="muted small" style={{ marginTop: 8 }}>
                tunables: {Object.entries(meta.config).map(([k, v]) => `${k}=${v}`).join("  ")}
              </div>}
          </div>
          <div className="card">
            <h3>chaos history — what you did to it</h3>
            <div className="scroll">
              {hist.map((e, i) =>
                <div className="chk" key={i}>
                  <span className="actual">{e.ts.replace("T", " ").slice(11, 19)}Z</span>
                  <span className="name">{e.kind}</span>
                  <span className="actual">{e.detail}</span>
                </div>)}
              {!hist.length && <div className="muted small">no chaos events yet — flip a mode</div>}
            </div>
          </div>
        </div>}

      <FixturesPanel langs={props.langs} />
    </div>
  );
}

/* ---------------- chaos fixtures (docker + minikube) ---------------- */

const MK_SETUP = `# from the repo root — bring up the whole fixture fleet
minikube start
minikube image build -t chaos-target:latest monitored-app/chaos-target
kubectl apply -f monitored-app/k8s/minikube.yaml
kubectl get pods -A -w                               # watch the chaos-* pods come up

# then register the fleets with any running monitor impl:
monitored-app/scripts/seed-monitors.sh http://localhost:8082   # :8081 java · :8082 go · :8083 py · :8084 ts`;

const MK_IMAGE_FIX = `# pods can't pull the image — build it inside minikube, then restart:
minikube image build -t chaos-target:latest monitored-app/chaos-target
kubectl rollout restart -n chaos-east deploy/chaos-target
kubectl rollout restart -n chaos-west deploy/chaos-target deploy/chaos-target-bad`;

const MK_TUNNEL = `# macOS docker driver: minikube's IP isn't routable from the host.
# NodePorts work inside the node — expose them on 127.0.0.1 with:
minikube tunnel                                    # keep running (sudo)
# or one service at a time:
minikube service chaos-target-nodeport -n chaos-east --url
minikube service chaos-target-nodeport -n chaos-west --url`;

function MetaLine(props: { meta: ChaosMeta }) {
  const m = props.meta;
  return <span> · mode <span className={"st-" + (m.mode === "healthy" ? "UP" : "DOWN")}>{m.mode}</span>
    {" "}· {m.requests_seen} polls · {m.instance}
    {m.paused ? " · paused" : ""}</span>;
}

function PodTable(props: { fleets: MkFleet[]; showNs?: boolean }) {
  const rows = props.fleets.flatMap(f => f.pods.map(p => ({ ...p, ns: f.namespace })));
  if (!rows.length) return null;
  return (
    <div className="scroll"><table>
      <thead><tr><th>pod</th>{props.showNs && <th>ns</th>}<th>ready</th><th>status</th><th>restarts</th><th>mode</th><th>fleet</th></tr></thead>
      <tbody>
        {rows.map(p => (
          <tr key={p.name}>
            <td>{p.name}</td>
            {props.showNs && <td className="muted">{p.ns}</td>}
            <td className={p.ready ? "ok-1" : "ok-0"}>{p.ready ? "✓" : "✗"}</td>
            <td className={p.ready ? "" : "st-DOWN"}>{p.status}</td>
            <td>{p.restarts}</td>
            <td>{p.mode || ""}</td>
            <td className="muted">{p.fleet}</td>
          </tr>))}
      </tbody>
    </table></div>
  );
}

function FixturesPanel(props: { langs?: Lang[] }) {
  const [fx, setFx] = useState<Fixtures | null>(null);
  const [probeFailed, setProbeFailed] = useState(false);
  useEffect(() => {
    let alive = true;
    const pull = () => fetch("/api/fixtures")
      .then(r => { if (!r.ok) throw new Error("HTTP " + r.status); return r.json(); })
      .then(d => { if (alive) { setFx(d); setProbeFailed(false); } })
      .catch(() => { if (alive) setProbeFailed(true); });
    pull();
    const t = setInterval(pull, 10000); // slower cadence — shells out to minikube/kubectl/docker
    return () => { alive = false; clearInterval(t); };
  }, []);

  const mk = fx?.minikube || null;
  const fleets = mk?.fleets || [];
  const dockers = fx?.docker || [];
  const anyFleetUp = fleets.some(f => f.up);
  const anyPodsReady = fleets.some(f => f.ready_pods > 0);
  const anyPods = fleets.some(f => f.pods.length > 0);
  const imageStuck = fleets.some(f =>
    f.pods.some(p => !p.ready && /pull|image/i.test(p.status)));

  return (
    <div>
      {/* ---- docker chaos containers: what's actually live right now ---- */}
      <div className="card wide" style={{ marginTop: 16 }}>
        <h2>Docker chaos fixtures
          <span className="sub">chaos-targets-* on localhost</span></h2>
        <div className="badges">
          {!fx && !probeFailed && <Badge cls="b-grey">probing…</Badge>}
          {probeFailed && <Badge cls="b-amber">restart dashboard.py — this build predates /api/fixtures</Badge>}
          {fx && !dockers.length && <Badge cls="b-grey">no chaos-targets containers running</Badge>}
          {dockers.map(d =>
            <Badge key={d.name} cls={d.up ? "b-green" : "b-red"}>
              {d.name.replace("chaos-targets-", "").replace("-1", "")} :{d.port}</Badge>)}
        </div>
        {dockers.length > 0 &&
          <div className="scroll"><table>
            <thead><tr><th>container</th><th>url</th><th>up</th><th>mode</th><th>instance</th><th>polls</th></tr></thead>
            <tbody>
              {dockers.map(d => (
                <tr key={d.name}>
                  <td>{d.name}</td>
                  <td><a className="titlelink" href={d.url} target="_blank">{d.url}</a></td>
                  <td className={d.up ? "ok-1" : "ok-0"}>{d.up ? "✓" : "✗"}</td>
                  <td>{d.meta ? d.meta.mode : "—"}</td>
                  <td className="muted">{d.meta?.instance || ""}</td>
                  <td>{d.meta?.requests_seen ?? ""}</td>
                </tr>))}
            </tbody>
          </table></div>}
        {dockers.length > 0 &&
          <div>
            <div className="down-note">
              Live now — every row is a full chaos-target: poll endpoints to monitor,
              and a /chaos/* control plane to flip.
            </div>
            <pre className="logs">{`# break one, watch a monitor row go DOWN:
curl -X POST 'http://127.0.0.1:${dockers[0].port}/chaos/mode?mode=down'
curl -X POST 'http://127.0.0.1:${dockers[0].port}/chaos/reset'
curl -X POST 'http://127.0.0.1:${dockers[0].port}/chaos/scenario?name=chaos-tour'

# or register it as a monitor on any running impl:
curl -X POST http://localhost:8082/api/v1/monitors -H 'Content-Type: application/json' \\
  -d '{"name":"docker-fixture","url":"http://host.docker.internal:${dockers[0].port}/health","interval_seconds":5,"timeout_ms":2000,"failure_threshold":3}'`}</pre>
          </div>}
      </div>

      {/* ---- minikube fleets ---- */}
      <div className="card wide" style={{ marginTop: 16 }}>
        <h2>Minikube fixture fleets
          <span className="sub">chaos-east :30080 · chaos-west :30081</span></h2>
        <div className="badges">
          {!mk && !probeFailed && <Badge cls="b-grey">probing…</Badge>}
          {mk && !mk.installed && <Badge cls="b-red">minikube not installed</Badge>}
          {mk && mk.installed && !mk.running && <Badge cls="b-red">minikube stopped</Badge>}
          {mk?.running && <Badge cls="b-green">minikube running · {mk.ip}</Badge>}
          {fleets.map(f =>
            <Badge key={f.namespace} cls={f.up ? "b-green" : f.ready_pods ? "b-amber" : "b-red"}>
              {f.namespace} {f.up ? "UP" : f.ready_pods ? `${f.ready_pods}/${f.pods.length} ready` : "DOWN"}</Badge>)}
        </div>

        {/* fleets reachable → live data + what-to-try hints */}
        {mk?.running && anyFleetUp &&
          <div>
            {fleets.map(f => (
              <div key={f.namespace} style={{ marginTop: 10 }}>
                <h3>{f.namespace} — <a className="titlelink" href={f.url || "#"}>{f.url}</a>
                  {f.meta && <MetaLine meta={f.meta} />}</h3>
                <PodTable fleets={[f]} />
              </div>
            ))}
            <div className="down-note">
              Try this next — east is 3 jittered replicas (UP with latency wobble);
              west hides 1 born-bad flapping pod behind the Service (intermittent 503s).
            </div>
            <pre className="logs">{`# register the fleets with a running monitor impl (pick its port):
monitored-app/scripts/seed-monitors.sh http://localhost:8082

# spot the bad pod, then break or heal one in place:
monitored-app/scripts/audit.sh
monitored-app/scripts/chaos-pod.sh chaos-west <pod-name> down
monitored-app/scripts/chaos-pod.sh chaos-west <pod-name> reset`}</pre>
          </div>}

        {/* pods running but NodePort unreachable → docker-driver tunnel hint */}
        {mk?.running && !anyFleetUp && anyPodsReady &&
          <div>
            <PodTable fleets={fleets} showNs />
            <pre className="logs">{MK_TUNNEL}</pre>
          </div>}

        {/* pods exist but can't start → image fix; none at all → full setup */}
        {mk?.running && !anyFleetUp && !anyPodsReady &&
          <div>
            <PodTable fleets={fleets} showNs />
            <pre className="logs">{imageStuck || !anyPods ? MK_IMAGE_FIX + "\n\n" + MK_SETUP : MK_SETUP}</pre>
          </div>}

        {/* no minikube → full run instructions */}
        {mk && !mk.running &&
          <div>
            <div className="down-note">
              {mk.installed
                ? "minikube is installed but not running — start it to get the full chaos fleet:"
                : "minikube not found on PATH — install it (brew install minikube), then:"}
            </div>
            <pre className="logs">{MK_SETUP}</pre>
          </div>}
      </div>
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
      {item("chaos", "Chaos")}
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

  const chaos = (payload: Record<string, unknown>) => {
    setBusy("chaos");
    fetch("/api/targets/chaos/chaos", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    })
      .then(r => r.json())
      .then(d => { if (!d.ok) setErr("chaos: " + (d.detail || "HTTP " + d.status)); })
      .catch(e => setErr(String(e)))
      .finally(() => { setBusy(null); refresh(); });
  };

  const langByKey = (k: Page) => dash?.languages.find(l => l.key === k);
  const chaosTarget = dash?.targets?.find(t => t.key === "chaos");

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
        {dash && page === "chaos" && (chaosTarget
          ? <ChaosPage T={chaosTarget} langs={dash.languages} onAction={action} onChaos={chaos} busy={busy} />
          : <FixturesPanel />)}
        {dash && page === "db" && <DbPage db={dash.db} />}
      </main>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
window.__mounted = true;
