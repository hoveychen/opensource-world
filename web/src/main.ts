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

function compact(n: number): string {
  if (n >= 1e9) return (n / 1e9).toFixed(1).replace(/\.0$/, "") + "B";
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e5 ? 0 : 1).replace(/\.0$/, "") + "k";
  return String(n);
}
function grouped(n: number): string {
  return n.toLocaleString("en-US");
}
function esc(s: string): string {
  return s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]!);
}

// GitHub Linguist language colors (common subset; others fall back to grey).
const LANG_COLORS: Record<string, string> = {
  JavaScript: "#f1e05a", TypeScript: "#3178c6", Python: "#3572A5", Java: "#b07219",
  Go: "#00ADD8", Rust: "#dea584", C: "#555555", "C++": "#f34b7d", "C#": "#178600",
  Ruby: "#701516", PHP: "#4F5D95", Shell: "#89e051", HTML: "#e34c26", CSS: "#563d7c",
  Swift: "#F05138", Kotlin: "#A97BFF", Dart: "#00B4AB", Scala: "#c22d40",
  "Jupyter Notebook": "#DA5B0B", Vue: "#41b883", Lua: "#000080", Haskell: "#5e5086",
  Elixir: "#6e4a7e", Clojure: "#db5855", "Objective-C": "#438eff", R: "#198CE7",
  Perl: "#0298c3", PowerShell: "#012456", Zig: "#ec915c", Nix: "#7e7eff",
  TeX: "#3D6117", Markdown: "#083fa1", Svelte: "#ff3e00", "Vim Script": "#199f4b",
};
function langDot(lang: string): string {
  if (!lang) return "";
  const c = LANG_COLORS[lang] ?? "#8b949e";
  return `<span class="lang-dot" style="background:${c}"></span>`;
}

// Octicons (16) used inline.
const STAR = `<svg class="ico" aria-hidden="true" height="14" width="14" viewBox="0 0 16 16" fill="currentColor"><path d="M8 .25a.75.75 0 0 1 .673.418l1.882 3.815 4.21.612a.75.75 0 0 1 .416 1.279l-3.046 2.97.719 4.192a.751.751 0 0 1-1.088.791L8 12.347l-3.766 1.98a.75.75 0 0 1-1.088-.79l.72-4.194L.818 6.374a.75.75 0 0 1 .416-1.28l4.21-.611L7.327.668A.75.75 0 0 1 8 .25Z"/></svg>`;
const FORK = `<svg class="ico" aria-hidden="true" height="14" width="14" viewBox="0 0 16 16" fill="currentColor"><path d="M5 5.372v.878c0 .414.336.75.75.75h4.5a.75.75 0 0 0 .75-.75v-.878a2.25 2.25 0 1 1 1.5 0v.878a2.25 2.25 0 0 1-2.25 2.25h-1.5v2.128a2.251 2.251 0 1 1-1.5 0V8.5h-1.5A2.25 2.25 0 0 1 3.5 6.25v-.878a2.25 2.25 0 1 1 1.5 0ZM5 3.25a.75.75 0 1 0-1.5 0 .75.75 0 0 0 1.5 0Zm6.75.75a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5Zm-3 8.75a.75.75 0 1 0-1.5 0 .75.75 0 0 0 1.5 0Z"/></svg>`;
const MARK = `<svg height="20" width="20" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.38A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z"/></svg>`;
const MOON = `<svg class="i-moon" height="16" width="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M9.598 1.591a.749.749 0 0 1 .785-.175 7.001 7.001 0 1 1-8.967 8.967.75.75 0 0 1 .961-.96 5.5 5.5 0 0 0 7.046-7.046.75.75 0 0 1 .175-.786Zm1.616 1.945a7 7 0 0 1-7.678 7.678 5.499 5.499 0 1 0 7.678-7.678Z"/></svg>`;
const SUN = `<svg class="i-sun" height="16" width="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 12a4 4 0 1 1 0-8 4 4 0 0 1 0 8Zm0-1.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Zm5.657-8.157a.75.75 0 0 1 0 1.061l-1.061 1.06a.749.749 0 0 1-1.275-.326.749.749 0 0 1 .215-.734l1.06-1.06a.75.75 0 0 1 1.06 0Zm-9.193 9.193a.75.75 0 0 1 0 1.06l-1.06 1.061a.75.75 0 1 1-1.061-1.06l1.06-1.061a.75.75 0 0 1 1.061 0ZM8 0a.75.75 0 0 1 .75.75v1.5a.75.75 0 0 1-1.5 0V.75A.75.75 0 0 1 8 0ZM3 8a.75.75 0 0 1-.75.75H.75a.75.75 0 0 1 0-1.5h1.5A.75.75 0 0 1 3 8Zm13 0a.75.75 0 0 1-.75.75h-1.5a.75.75 0 0 1 0-1.5h1.5A.75.75 0 0 1 16 8Zm-8 5a.75.75 0 0 1 .75.75v1.5a.75.75 0 0 1-1.5 0v-1.5A.75.75 0 0 1 8 13Zm3.536-1.464a.75.75 0 0 1 1.06 0l1.061 1.06a.75.75 0 0 1-1.06 1.061l-1.061-1.06a.75.75 0 0 1 0-1.061ZM2.343 2.343a.75.75 0 0 1 1.061 0l1.06 1.061a.751.751 0 0 1-.018 1.042.751.751 0 0 1-1.042.018l-1.06-1.06a.75.75 0 0 1 0-1.06Z"/></svg>`;

function currentTheme(): "light" | "dark" {
  const saved = localStorage.getItem("theme");
  if (saved === "light" || saved === "dark") return saved;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}
function applyTheme(t: "light" | "dark") {
  document.documentElement.dataset.theme = t;
}
applyTheme(currentTheme());

// Render the official github-buttons Star widget into `.star-badge`, forcing
// its colour scheme to match the site's *current* theme (not the OS one), then
// (re)run buttons.js. The widget reads data-color-scheme at render time and
// replaces the anchor with an iframe, so to follow the manual theme toggle we
// rebuild a fresh anchor and re-execute the script on every theme change.
function renderStarBadge() {
  const wrap = document.querySelector(".star-badge");
  if (!wrap) return;
  const theme = document.documentElement.dataset.theme === "light" ? "light" : "dark";
  wrap.innerHTML = `<a class="github-button" href="https://github.com/hoveychen/opensource-world"
    data-color-scheme="${theme}" data-icon="octicon-star" data-size="large" data-show-count="true"
    aria-label="Star hoveychen/opensource-world on GitHub">Star</a>`;
  // Re-adding the script element re-executes it; its render pass scans for
  // `a.github-button` and converts the fresh anchor with the new scheme.
  document.getElementById("gh-buttons-js")?.remove();
  const s = document.createElement("script");
  s.id = "gh-buttons-js";
  s.async = true;
  s.src = "https://buttons.github.io/buttons.js";
  document.body.appendChild(s);
}

function wireTheme() {
  const btn = document.getElementById("theme-toggle");
  if (!btn) return;
  btn.addEventListener("click", () => {
    const next = document.documentElement.dataset.theme === "light" ? "dark" : "light";
    applyTheme(next);
    localStorage.setItem("theme", next);
    renderStarBadge();
  });
}

function topbar(): string {
  return `
  <div class="topbar">
    <div class="topbar-inner">
      <span class="mark">${MARK}</span>
      <span class="path"><a href="https://github.com/hoveychen" target="_blank" rel="noopener">hoveychen</a><span class="sep">/</span><a href="https://github.com/hoveychen/opensource-world" target="_blank" rel="noopener">opensource-world</a></span>
      <span class="badge">Public</span>
      <span class="star-badge"></span>
      <button class="theme-toggle" id="theme-toggle" type="button" aria-label="Toggle color theme" title="Toggle light/dark">${MOON}${SUN}</button>
    </div>
  </div>`;
}

function hero(meta: Meta): string {
  const date = new Date(meta.generated_at).toLocaleDateString("en-US", {
    year: "numeric", month: "short", day: "numeric",
  });
  return `
  <header class="hero">
    <div class="title">
      <h1>opensource-<b>world</b></h1>
      <span class="public">survey</span>
    </div>
    <p class="desc">
      Every public, non-fork repository on GitHub with at least ten stars —
      surveyed, catalogued, and charted.
    </p>
    <div class="stat-row">
      <span class="stat">${STAR} <b>${grouped(meta.total_repos)}</b> repositories</span>
      <span class="stat"><b>${compact(meta.max_stars)}</b> max stars</span>
      <span class="stat"><b>${compact(meta.enriched)}</b> enriched · ecosyste.ms</span>
      <span class="stat">updated <b>${date}</b></span>
    </div>
  </header>`;
}

function box(num: string, title: string, blurb: string, body: string): string {
  return `
  <section class="section">
    <div class="box-header">
      <div class="eyebrow">${num}</div>
      <h2>${title}</h2>
      <p>${blurb}</p>
    </div>
    <div class="box-body">${body}</div>
  </section>`;
}

// ---------- interactive star ranking ----------
const SHOW = 40;
let ALL_REPOS: TopRepo[] = [];

function rankRowsHTML(repos: TopRepo[]): string {
  if (!repos.length) return `<div class="empty">No repositories match that filter.</div>`;
  return repos
    .slice(0, SHOW)
    .map((r, i) => {
      const lang = r.language
        ? `<span class="lang">${langDot(r.language)}${esc(r.language)}</span>`
        : "";
      const desc = r.description ? `<div class="desc">${esc(r.description)}</div>` : "";
      return `
      <a class="rank-row" href="${esc(r.html_url)}" target="_blank" rel="noopener">
        <div class="top">
          <span class="rk">${i + 1}</span>
          <span class="name">${esc(r.full_name)}</span>
        </div>
        ${desc}
        <div class="meta">
          ${lang}
          <span class="stars">${STAR} ${grouped(r.stars)}</span>
          <span class="forks">${FORK} ${grouped(r.forks)}</span>
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
  const body = `
    <div class="controls">
      <input id="rank-search" type="search" placeholder="Search name, description or topic…" autocomplete="off" />
      <select id="rank-lang">
        <option value="">All languages</option>
        ${options}
      </select>
      <span id="rank-count" class="count"></span>
    </div>
    <div class="rank" id="rank-list">${rankRowsHTML(repos)}</div>`;
  return box(
    "Ranking",
    "Most-starred repositories",
    `Search by name, description or topic, or filter to a single language. Top ${SHOW} matches shown.`,
    body
  );
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
        <span class="lang-name">${langDot(l.language)}${esc(l.language)}</span>
        <span class="lang-bar"><span class="lang-fill"></span></span>
        <span class="lang-val"><b>${compact(l.repos)}</b> repos · ${compact(l.stars)} ★</span>
      </div>`;
    })
    .join("");
  return box(
    "Languages",
    "Language distribution",
    "Which languages the surveyed repositories are written in, by count — with total stars alongside.",
    `<div class="lang-chart">${rows}</div>`
  );
}

// ---------- trends ----------
function trends(data: YearBucket[]): string {
  const pts = data.filter((d) => d.year >= 2007 && d.year <= 2100);
  const W = 1000, H = 360, padX = 14, padTop = 24, padBot = 40;
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
    .map((d, i) => (i % 2 === 0 || i === n - 1 ? `<text x="${x(i)}" y="${H - 12}" class="ax">${d.year}</text>` : ""))
    .join("");
  const dots = pts.map((d, i) => `<circle cx="${x(i)}" cy="${yStar(d.stars)}" r="2.4" class="dot" />`).join("");
  const body = `
    <div class="legend">
      <span><i style="background:var(--success)"></i> total stars (by birth year)</span>
      <span><i style="background:var(--accent)"></i> repositories created</span>
    </div>
    <div class="chart-wrap">
      <svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Repositories and stars by creation year">
        <defs>
          <linearGradient id="starfill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="rgba(63,185,80,0.25)" />
            <stop offset="100%" stop-color="rgba(63,185,80,0)" />
          </linearGradient>
        </defs>
        <polygon points="${starArea}" fill="url(#starfill)" />
        <polyline class="draw" pathLength="1" points="${starLine}" fill="none" stroke="var(--success)"
          stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round" />
        <polyline class="draw draw-2" pathLength="1" points="${repoLine}" fill="none" stroke="var(--accent)"
          stroke-width="2" stroke-dasharray="3 4" stroke-linecap="round" />
        ${dots}
        ${ticks}
      </svg>
    </div>`;
  return box(
    "Trends",
    "Growth over time",
    "Repositories grouped by the year they were created — how many appeared, and how much light they have since gathered.",
    body
  );
}

// ---------- topics ----------
function topicsView(topics: TopicCount[]): string {
  const items = topics
    .slice(0, 80)
    .map(
      (t) =>
        `<a class="topic" href="https://github.com/topics/${encodeURIComponent(t.topic)}" target="_blank" rel="noopener"
        title="${grouped(t.repos)} repos · ${grouped(t.stars)} stars">${esc(t.topic)}<span class="cnt">${compact(t.repos)}</span></a>`
    )
    .join("");
  return box(
    "Topics",
    "Popular topics",
    "The topics communities tag their repositories with, most-used first. Each pill shows how many repositories carry it.",
    `<div class="constellation">${items}</div>`
  );
}

function footer(meta: Meta): string {
  return `
  <footer class="foot">
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
  app.innerHTML = `<div class="loading">Loading survey…</div>`;
  try {
    const [meta, repos, yr, topics, langs] = await Promise.all([
      getJSON<Meta>("meta.json"),
      getJSON<TopRepo[]>("top_repos.json"),
      getJSON<YearBucket[]>("trends.json"),
      getJSON<TopicCount[]>("topics.json"),
      getJSON<LanguageCount[]>("languages.json"),
    ]);
    app.insertAdjacentHTML("beforebegin", topbar());
    wireTheme();
    renderStarBadge();
    app.innerHTML =
      hero(meta) +
      rankings(repos, langs) +
      languages(langs) +
      trends(yr) +
      topicsView(topics) +
      footer(meta);
    wireRankings();
    reveal();
  } catch (err) {
    app.innerHTML = `<div class="loading">Could not load the survey data.<br/>${esc(String(err))}</div>`;
  }
}

main();
