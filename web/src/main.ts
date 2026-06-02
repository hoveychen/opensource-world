import "./style.css";

type Meta = {
  generated_at: string;
  total_repos: number;
  enriched: number;
  min_stars: number;
  max_stars: number;
};
type TopRepo = {
  full_name: string;
  stars: number;
  forks: number;
  language: string;
  description: string;
  html_url: string;
  topics: string[];
};
type YearBucket = { year: number; repos: number; stars: number };
type TopicCount = { topic: string; repos: number; stars: number };
type LanguageCount = { language: string; repos: number; stars: number };

const BASE = import.meta.env.BASE_URL;
const dataURL = (name: string) => `${BASE}data/${name}`;

async function getJSON<T>(name: string): Promise<T> {
  const res = await fetch(dataURL(name));
  if (!res.ok) throw new Error(`failed to load ${name}: ${res.status}`);
  return res.json() as Promise<T>;
}

/** Compact number: 245314 -> "245K", 1700000 -> "1.7M". */
function compact(n: number): string {
  if (n >= 1e9) return (n / 1e9).toFixed(1).replace(/\.0$/, "") + "B";
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e5 ? 0 : 1).replace(/\.0$/, "") + "K";
  return String(n);
}
function grouped(n: number): string {
  return n.toLocaleString("en-US");
}
function esc(s: string): string {
  return s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]!);
}

function hero(meta: Meta): string {
  const date = new Date(meta.generated_at).toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
  return `
  <header class="hero section">
    <div class="eyebrow">The Open-Source Almanac</div>
    <h1>A star chart of the<br /><em>open-source cosmos</em>.</h1>
    <p class="lede">
      Every public, non-fork repository on GitHub with at least ten stars —
      surveyed, catalogued, and plotted. ${grouped(meta.total_repos)} bodies of
      light and counting.
    </p>
    <div class="stat-row">
      <div class="stat">
        <div class="num"><span class="accent">${compact(meta.total_repos)}</span></div>
        <div class="lbl">repositories charted</div>
      </div>
      <div class="stat">
        <div class="num">${compact(meta.max_stars)}</div>
        <div class="lbl">brightest (max stars)</div>
      </div>
      <div class="stat">
        <div class="num">${compact(meta.enriched)}</div>
        <div class="lbl">enriched via ecosyste.ms</div>
      </div>
      <div class="stat">
        <div class="num" style="font-size:1.1rem">${date}</div>
        <div class="lbl">last survey</div>
      </div>
    </div>
  </header>`;
}

// ---------- interactive star ranking ----------
const SHOW = 40;
let ALL_REPOS: TopRepo[] = [];

function rankRowsHTML(repos: TopRepo[]): string {
  if (!repos.length) {
    return `<div class="empty">No repositories match that filter.</div>`;
  }
  const max = repos[0].stars || 1;
  return repos
    .slice(0, SHOW)
    .map((r, i) => {
      const w = ((r.stars / max) * 100).toFixed(1);
      const lang = r.language ? `<span class="lang">${esc(r.language)}</span>` : "";
      const desc = r.description ? `<div class="desc">${esc(r.description)}</div>` : "";
      return `
      <a class="rank-row" style="--w:${w}%" href="${esc(r.html_url)}" target="_blank" rel="noopener">
        <div class="rank-num">${String(i + 1).padStart(2, "0")}</div>
        <div class="rank-main">
          <div class="name">${esc(r.full_name)}</div>
          ${desc}
        </div>
        <div class="rank-meta">
          <span class="stars">★ ${grouped(r.stars)}</span>
          ${lang}
        </div>
      </a>`;
    })
    .join("");
}

function applyFilter(query: string, lang: string): TopRepo[] {
  const q = query.trim().toLowerCase();
  return ALL_REPOS.filter((r) => {
    if (lang && r.language !== lang) return false;
    if (!q) return true;
    return (
      r.full_name.toLowerCase().includes(q) ||
      r.description.toLowerCase().includes(q) ||
      r.topics.some((t) => t.includes(q))
    );
  });
}

function rankings(repos: TopRepo[], langs: LanguageCount[]): string {
  ALL_REPOS = repos;
  const options = langs
    .map((l) => `<option value="${esc(l.language)}">${esc(l.language)} · ${compact(l.repos)}</option>`)
    .join("");
  return `
  <section class="section">
    <div class="section-head">
      <div class="eyebrow">I · Brightest bodies</div>
      <h2>The star ranking</h2>
      <p>The most-starred repositories in the survey. Search by name, description
        or topic, or filter to a single language — the top ${SHOW} matches are drawn,
        each row's glow to scale against the brightest.</p>
    </div>
    <div class="controls">
      <input id="rank-search" type="search" placeholder="search name, description, topic…" autocomplete="off" />
      <select id="rank-lang">
        <option value="">All languages</option>
        ${options}
      </select>
      <span id="rank-count" class="count"></span>
    </div>
    <div class="rank" id="rank-list">${rankRowsHTML(repos)}</div>
  </section>`;
}

function wireRankings() {
  const search = document.getElementById("rank-search") as HTMLInputElement | null;
  const select = document.getElementById("rank-lang") as HTMLSelectElement | null;
  const list = document.getElementById("rank-list");
  const count = document.getElementById("rank-count");
  if (!search || !select || !list || !count) return;

  const update = () => {
    const filtered = applyFilter(search.value, select.value);
    list.innerHTML = rankRowsHTML(filtered);
    count.textContent = `${grouped(filtered.length)} match${filtered.length === 1 ? "" : "es"}`;
  };
  search.addEventListener("input", update);
  select.addEventListener("change", update);
  count.textContent = `${grouped(ALL_REPOS.length)} repos`;
}

// ---------- language distribution ----------
function languages(langs: LanguageCount[]): string {
  const top = langs.slice(0, 18);
  const max = Math.max(...top.map((l) => l.repos), 1);
  const rows = top
    .map((l, i) => {
      const w = ((l.repos / max) * 100).toFixed(1);
      return `
      <div class="lang-row" style="--w:${w}%; --i:${i}">
        <span class="lang-name">${esc(l.language)}</span>
        <span class="lang-bar"><span class="lang-fill"></span></span>
        <span class="lang-val">${compact(l.repos)} <em>· ${compact(l.stars)}★</em></span>
      </div>`;
    })
    .join("");
  return `
  <section class="section">
    <div class="section-head">
      <div class="eyebrow">II · Tongues of the sky</div>
      <h2>Language distribution</h2>
      <p>Which languages the surveyed repositories are written in — by count, with
        their total gathered starlight alongside.</p>
    </div>
    <div class="lang-chart">${rows}</div>
  </section>`;
}

// ---------- trends ----------
function trends(data: YearBucket[]): string {
  const pts = data.filter((d) => d.year >= 2007 && d.year <= 2100);
  const W = 1000;
  const H = 380;
  const padX = 14;
  const padTop = 24;
  const padBot = 40;
  const n = pts.length;
  const maxStars = Math.max(...pts.map((d) => d.stars), 1);
  const maxRepos = Math.max(...pts.map((d) => d.repos), 1);
  const x = (i: number) => padX + (i * (W - 2 * padX)) / Math.max(n - 1, 1);
  const yStar = (v: number) => padTop + (1 - v / maxStars) * (H - padTop - padBot);
  const yRepo = (v: number) => padTop + (1 - v / maxRepos) * (H - padTop - padBot);

  const starLine = pts.map((d, i) => `${x(i)},${yStar(d.stars)}`).join(" ");
  const starArea = `${padX},${H - padBot} ${starLine} ${x(n - 1)},${H - padBot}`;
  const repoLine = pts.map((d, i) => `${x(i)},${yRepo(d.repos)}`).join(" ");

  const ticks = pts
    .map((d, i) =>
      i % 2 === 0 || i === n - 1
        ? `<text x="${x(i)}" y="${H - 12}" class="ax">${d.year}</text>`
        : ""
    )
    .join("");
  const dots = pts
    .map((d, i) => `<circle cx="${x(i)}" cy="${yStar(d.stars)}" r="2.6" class="dot" />`)
    .join("");

  return `
  <section class="section">
    <div class="section-head">
      <div class="eyebrow">III · The widening sky</div>
      <h2>Growth over time</h2>
      <p>Repositories grouped by the year they were created — how many appeared,
        and how much light (stars) they have since gathered.</p>
    </div>
    <div class="legend">
      <span><i style="background:var(--gold)"></i> total stars (by birth year)</span>
      <span><i style="background:var(--cyan)"></i> repositories created</span>
    </div>
    <div class="chart-wrap">
      <svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Repositories and stars by creation year">
        <defs>
          <linearGradient id="starfill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="rgba(232,176,75,0.35)" />
            <stop offset="100%" stop-color="rgba(232,176,75,0)" />
          </linearGradient>
        </defs>
        <polygon points="${starArea}" fill="url(#starfill)" />
        <polyline class="draw" pathLength="1" points="${starLine}" fill="none" stroke="var(--gold)"
          stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round" />
        <polyline class="draw draw-2" pathLength="1" points="${repoLine}" fill="none" stroke="var(--cyan)"
          stroke-width="2" stroke-dasharray="2 5" stroke-linecap="round" opacity="0.9" />
        ${dots}
        ${ticks}
      </svg>
    </div>
  </section>`;
}

// ---------- topic constellation ----------
function constellation(topics: TopicCount[]): string {
  const top = topics.slice(0, 80);
  const max = Math.max(...top.map((t) => t.repos), 1);
  const min = Math.min(...top.map((t) => t.repos), 1);
  const norm = (v: number) =>
    (Math.sqrt(v) - Math.sqrt(min)) / (Math.sqrt(max) - Math.sqrt(min) || 1);
  const size = (v: number) => (0.85 + norm(v) * 1.9).toFixed(2);
  const opacity = (v: number) => (0.45 + norm(v) * 0.55).toFixed(2);
  const items = top
    .map(
      (t) =>
        `<a class="topic" style="font-size:${size(t.repos)}rem;color:rgba(236,227,208,${opacity(
          t.repos
        )})" href="https://github.com/topics/${encodeURIComponent(t.topic)}" target="_blank" rel="noopener"
        title="${grouped(t.repos)} repos · ${grouped(t.stars)} stars">${esc(t.topic)}<span class="cnt">${compact(
          t.repos
        )}</span></a>`
    )
    .join("");
  return `
  <section class="section">
    <div class="section-head">
      <div class="eyebrow">IV · Named constellations</div>
      <h2>Topic constellation</h2>
      <p>The topics communities tag themselves with, sized by how many
        repositories carry each. Larger means more crowded sky.</p>
    </div>
    <div class="constellation">${items}</div>
  </section>`;
}

function footer(meta: Meta): string {
  return `
  <footer class="foot section">
    <span>Surveyed ${grouped(meta.total_repos)} repositories · stars ≥ ${meta.min_stars}</span>
    <span>Data: GitHub Search + <a href="https://ecosyste.ms" target="_blank" rel="noopener">ecosyste.ms</a> ·
      <a href="https://github.com/hoveychen/opensource-world" target="_blank" rel="noopener">source</a></span>
  </footer>`;
}

function reveal() {
  const obs = new IntersectionObserver(
    (entries) => {
      entries.forEach((e) => {
        if (e.isIntersecting) {
          e.target.classList.add("in");
          obs.unobserve(e.target);
        }
      });
    },
    { threshold: 0.1 }
  );
  document.querySelectorAll(".section").forEach((s) => obs.observe(s));
}

async function main() {
  const app = document.getElementById("app")!;
  app.innerHTML = `<div class="loading">Charting the sky…</div>`;
  try {
    const [meta, repos, yr, topics, langs] = await Promise.all([
      getJSON<Meta>("meta.json"),
      getJSON<TopRepo[]>("top_repos.json"),
      getJSON<YearBucket[]>("trends.json"),
      getJSON<TopicCount[]>("topics.json"),
      getJSON<LanguageCount[]>("languages.json"),
    ]);
    app.innerHTML =
      hero(meta) +
      rankings(repos, langs) +
      languages(langs) +
      trends(yr) +
      constellation(topics) +
      footer(meta);
    wireRankings();
    reveal();
  } catch (err) {
    app.innerHTML = `<div class="loading">Could not load the survey data.<br/>${esc(
      String(err)
    )}</div>`;
  }
}

main();
