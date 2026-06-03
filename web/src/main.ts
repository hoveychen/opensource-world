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
type CoverageBand = { lo: number; hi: number; label: string };
type CoverageCell = { repos: number; cov: number };
type Coverage = { bands: CoverageBand[]; months: string[]; cells: CoverageCell[][] };
type DDSBucket = { lo: number; hi: number; repos: number };
type FileAdoption = { kind: string; present: number; rate: number };
type Health = {
  enriched_stats: number;
  dds_buckets: DDSBucket[];
  files: FileAdoption[];
  scorecard_count: number;
  scorecard_avg: number;
};

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
const SEARCH = `<svg class="ico" aria-hidden="true" height="14" width="14" viewBox="0 0 16 16" fill="currentColor"><path d="M10.68 11.74a6 6 0 0 1-7.922-8.982 6 6 0 0 1 8.982 7.922l3.04 3.041a.749.749 0 0 1-.326 1.275.749.749 0 0 1-.734-.215ZM11.5 7a4.499 4.499 0 1 0-8.997 0A4.499 4.499 0 0 0 11.5 7Z"/></svg>`;
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

function slug(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, "-");
}

function box(num: string, title: string, blurb: string, body: string): string {
  return `
  <section class="section" id="${slug(num)}">
    <div class="box-header">
      <div class="eyebrow">${num}</div>
      <h2>${title}</h2>
      <p>${blurb}</p>
    </div>
    <div class="box-body">${body}</div>
  </section>`;
}

// Sticky anchor nav across the six sections. The labels match each box()'s
// `num` eyebrow, so the hrefs line up with the section ids slug() produces.
const NAV_SECTIONS = ["Ranking", "Languages", "Trends", "Coverage", "Health", "Topics"];

function sectionNav(): string {
  const links = NAV_SECTIONS.map(
    (n) => `<a class="nav-link" href="#${slug(n)}" data-nav="${slug(n)}">${n}</a>`
  ).join("");
  return `<nav class="section-nav"><div class="section-nav-inner">${links}</div></nav>`;
}

// Highlight the nav link for whichever section is currently centred in the
// viewport (a lightweight scrollspy).
function wireNav() {
  const links = new Map<string, HTMLElement>();
  document.querySelectorAll<HTMLElement>(".nav-link").forEach((a) => {
    if (a.dataset.nav) links.set(a.dataset.nav, a);
  });
  if (!links.size) return;
  const obs = new IntersectionObserver(
    (entries) => {
      entries.forEach((e) => {
        if (e.isIntersecting) {
          links.forEach((l) => l.classList.remove("active"));
          links.get((e.target as HTMLElement).id)?.classList.add("active");
        }
      });
    },
    { rootMargin: "-40% 0px -55% 0px", threshold: 0 }
  );
  document.querySelectorAll(".section").forEach((s) => obs.observe(s));
}

// ---------- interactive star ranking ----------
const SHOW = 40;
const LANG_SHOWN = 12;
const TOPIC_SHOWN = 16;
let ALL_REPOS: TopRepo[] = [];
// Active filter state, shared between the chip facets, the search box and the
// Topics-section pills (which feed topics in via filterByTopic).
const FILTER = { q: "", lang: "", topics: new Set<string>() };
// wireRankings() publishes its re-render here so outside callers (the Topics
// constellation) can apply a filter and refresh the list.
let rerankList: (() => void) | null = null;

type ChipCount = { key: string; count: number };

// Chip counts are derived from the loaded top repos (not the global
// languages/topics aggregates) so every chip's count matches exactly what the
// ranking shows when clicked, and no chip can ever yield zero results.
function countBy(pick: (r: TopRepo) => string[]): ChipCount[] {
  const m = new Map<string, number>();
  for (const r of ALL_REPOS)
    for (const k of pick(r)) if (k) m.set(k, (m.get(k) ?? 0) + 1);
  return [...m.entries()]
    .map(([key, count]) => ({ key, count }))
    .sort((a, b) => b.count - a.count || a.key.localeCompare(b.key));
}
const langChips = () => countBy((r) => [r.language]);
const topicChips = () => countBy((r) => r.topics);

// Apply a topic filter from outside the ranking (the Topics constellation),
// then scroll the ranking into view so the effect is visible.
function filterByTopic(topic: string) {
  FILTER.topics.add(topic);
  rerankList?.();
  document.getElementById("ranking")?.scrollIntoView({ behavior: "smooth", block: "start" });
}

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

function applyFilter(): TopRepo[] {
  const q = FILTER.q.trim().toLowerCase();
  return ALL_REPOS.filter((r) => {
    if (FILTER.lang && r.language !== FILTER.lang) return false;
    for (const t of FILTER.topics) if (!r.topics.includes(t)) return false;
    if (!q) return true;
    return (
      r.full_name.toLowerCase().includes(q) ||
      r.description.toLowerCase().includes(q) ||
      r.topics.some((t) => t.includes(q))
    );
  });
}

function langChipHTML(c: ChipCount, extra: boolean): string {
  return `<button class="chip lang-chip${extra ? " chip-extra" : ""}" type="button" data-lang="${esc(c.key)}">${langDot(c.key)}<span class="chip-label">${esc(c.key)}</span><span class="chip-cnt">${compact(c.count)}</span></button>`;
}
function topicChipHTML(c: ChipCount, extra: boolean): string {
  return `<button class="chip topic-chip${extra ? " chip-extra" : ""}" type="button" data-topic="${esc(c.key)}"><span class="chip-hash">#</span><span class="chip-label">${esc(c.key)}</span><span class="chip-cnt">${compact(c.count)}</span></button>`;
}
function moreBtnHTML(kind: string, hidden: number): string {
  return hidden > 0
    ? `<button class="chip-more" type="button" data-more="${kind}" data-more-label="+${hidden} more">+${hidden} more</button>`
    : "";
}
function tokenHTML(kind: string, val: string, label: string): string {
  return `<button class="token" type="button" data-tkind="${kind}" data-tval="${esc(val)}" aria-label="Remove ${esc(label)} filter">${esc(label)}<span class="token-x" aria-hidden="true">×</span></button>`;
}

function rankings(repos: TopRepo[]): string {
  ALL_REPOS = repos;
  const langs = langChips();
  const topics = topicChips();
  const langHTML =
    `<button class="chip lang-chip lang-all active" type="button" data-lang="">All</button>` +
    langs.map((c, i) => langChipHTML(c, i >= LANG_SHOWN)).join("") +
    moreBtnHTML("lang", Math.max(0, langs.length - LANG_SHOWN));
  const topicHTML =
    topics.map((c, i) => topicChipHTML(c, i >= TOPIC_SHOWN)).join("") +
    moreBtnHTML("topic", Math.max(0, topics.length - TOPIC_SHOWN));
  const body = `
    <div class="controls">
      <div class="search-wrap">${SEARCH}<input id="rank-search" type="search" placeholder="Search name, description or topic…" autocomplete="off" /></div>
      <span id="rank-count" class="count"></span>
    </div>
    <div class="facets" id="rank-facets">
      <div class="facet-group" data-group="lang">
        <span class="facet-label">Language</span>
        <div class="chips">${langHTML}</div>
      </div>
      <div class="facet-group" data-group="topic">
        <span class="facet-label">Topics</span>
        <div class="chips">${topicHTML}</div>
      </div>
    </div>
    <div class="active-filters" id="rank-active" hidden></div>
    <div class="rank" id="rank-list">${rankRowsHTML(repos)}</div>`;
  return box(
    "Ranking",
    "Most-starred repositories",
    `Click a language or topic to filter — combine several to narrow down — or search by name. Top ${SHOW} matches shown.`,
    body
  );
}

function wireRankings() {
  const search = document.getElementById("rank-search") as HTMLInputElement | null;
  const facets = document.getElementById("rank-facets");
  const list = document.getElementById("rank-list");
  const count = document.getElementById("rank-count");
  const active = document.getElementById("rank-active");
  if (!search || !facets || !list || !count || !active) return;

  const syncChips = () => {
    facets.querySelectorAll<HTMLElement>(".lang-chip").forEach((b) => {
      b.classList.toggle("active", (b.dataset.lang ?? "") === FILTER.lang);
    });
    facets.querySelectorAll<HTMLElement>(".topic-chip").forEach((b) => {
      b.classList.toggle("active", FILTER.topics.has(b.dataset.topic ?? ""));
    });
  };

  const renderActive = () => {
    const tokens: string[] = [];
    if (FILTER.lang) tokens.push(tokenHTML("lang", FILTER.lang, FILTER.lang));
    FILTER.topics.forEach((t) => tokens.push(tokenHTML("topic", t, `#${t}`)));
    if (!tokens.length) {
      active.hidden = true;
      active.innerHTML = "";
      return;
    }
    active.hidden = false;
    active.innerHTML = tokens.join("") + `<button class="clear-all" type="button" data-clear>Clear all</button>`;
  };

  const render = () => {
    const filtered = applyFilter();
    list.innerHTML = rankRowsHTML(filtered);
    count.textContent = `${grouped(filtered.length)} ${filtered.length === 1 ? "repo" : "repos"}`;
    syncChips();
    renderActive();
  };
  rerankList = render;

  search.addEventListener("input", () => {
    FILTER.q = search.value;
    render();
  });

  facets.addEventListener("click", (e) => {
    const btn = (e.target as HTMLElement).closest("button");
    if (!btn) return;
    if (btn.dataset.more) {
      const grp = btn.closest(".facet-group");
      const expanded = grp?.classList.toggle("expanded");
      btn.textContent = expanded ? "show less" : btn.dataset.moreLabel ?? btn.textContent;
      return;
    }
    if (btn.classList.contains("lang-chip")) {
      const v = btn.dataset.lang ?? "";
      FILTER.lang = v === FILTER.lang ? "" : v;
      render();
    } else if (btn.classList.contains("topic-chip")) {
      const v = btn.dataset.topic ?? "";
      if (FILTER.topics.has(v)) FILTER.topics.delete(v);
      else FILTER.topics.add(v);
      render();
    }
  });

  active.addEventListener("click", (e) => {
    const btn = (e.target as HTMLElement).closest("button");
    if (!btn) return;
    if (btn.dataset.clear !== undefined) {
      FILTER.lang = "";
      FILTER.topics.clear();
    } else if (btn.dataset.tkind === "lang") {
      FILTER.lang = "";
    } else if (btn.dataset.tkind === "topic") {
      FILTER.topics.delete(btn.dataset.tval ?? "");
    }
    render();
  });

  render();
}

function wireTopics() {
  const c = document.querySelector(".constellation");
  if (!c) return;
  c.addEventListener("click", (e) => {
    const btn = (e.target as HTMLElement).closest<HTMLElement>(".topic");
    if (!btn || !btn.dataset.topic) return;
    filterByTopic(btn.dataset.topic);
  });
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
        `<button class="topic" type="button" data-topic="${esc(t.topic)}"
        title="${grouped(t.repos)} repos · ${grouped(t.stars)} stars — click to filter the ranking">${esc(t.topic)}<span class="cnt">${compact(t.repos)}</span></button>`
    )
    .join("");
  return box(
    "Topics",
    "Popular topics",
    "The topics communities tag their repositories with, most-used first. Click any pill to filter the ranking above by that topic.",
    `<div class="constellation">${items}</div>`
  );
}

// ---------- crawl coverage heatmap ----------
// A (star band x creation month) grid. Cell fill = repo density (green ramp,
// GitHub-contribution style); empty cells are split into "scanned, none found"
// vs "not yet crawled" using the per-cell coverage ratio from crawl_windows.
function coverageView(cov: Coverage): string {
  const { bands, months, cells } = cov;
  if (!bands.length || !months.length) {
    return box("Coverage", "Crawl coverage map", "No coverage data yet.", "");
  }
  const maxRepos = Math.max(1, ...cells.flat().map((c) => c.repos));
  const logMax = Math.log(maxRepos + 1);
  // 0 = empty; 1..4 = green ramp (GitHub levels), by log of repo count.
  const level = (repos: number): number =>
    repos <= 0 ? 0 : Math.min(4, Math.max(1, Math.ceil((4 * Math.log(repos + 1)) / logMax)));

  const cellHTML = (c: CoverageCell, band: CoverageBand, ym: string): string => {
    let cls: string;
    let state: string;
    if (c.repos > 0) {
      cls = `cov-cell lvl-${level(c.repos)}`;
      state = `${grouped(c.repos)} repos`;
    } else if (c.cov >= 0.5) {
      cls = "cov-cell scanned";
      state = "scanned · none found";
    } else {
      cls = "cov-cell uncrawled";
      state = c.cov > 0 ? `partly crawled (${Math.round(c.cov * 100)}%)` : "not yet crawled";
    }
    return `<i class="${cls}" title="${band.label} ★ · ${ym}\n${state}"></i>`;
  };

  // Rows top-to-bottom = high stars to low, so the dense high-star band sits at
  // the top like a ranking. bands come low->high, so iterate reversed.
  const rows = bands
    .map((_, i) => bands.length - 1 - i)
    .map((b) => {
      const tiles = months.map((m, c) => cellHTML(cells[b][c], bands[b], m)).join("");
      return `<div class="cov-row"><span class="cov-rlabel">${esc(bands[b].label)}</span><div class="cov-tiles">${tiles}</div></div>`;
    })
    .join("");

  // Year ticks: mark the first month of each year.
  const ticks = months
    .map((m, i) => {
      const [y, mo] = m.split("-");
      return mo === "01" || i === 0 ? `<span class="cov-tick" style="--c:${i}">${y}</span>` : "";
    })
    .join("");

  const legend = `
    <div class="cov-legend">
      <span class="cov-legend-grp">Density
        <i class="cov-cell scanned"></i>
        <i class="cov-cell lvl-1"></i>
        <i class="cov-cell lvl-2"></i>
        <i class="cov-cell lvl-3"></i>
        <i class="cov-cell lvl-4"></i>
        more
      </span>
      <span class="cov-legend-grp"><i class="cov-cell scanned"></i> scanned, none found</span>
      <span class="cov-legend-grp"><i class="cov-cell uncrawled"></i> not yet crawled</span>
    </div>`;

  const body = `
    ${legend}
    <div class="cov-scroll">
      <div class="cov-grid" style="--cols:${months.length}">
        ${rows}
        <div class="cov-row cov-axis"><span class="cov-rlabel"></span><div class="cov-tiles cov-ticks">${ticks}</div></div>
      </div>
    </div>`;
  return box(
    "Coverage",
    "Crawl coverage map",
    "Each tile is one star band over one month of repository births. Green shows how many repositories live there; pale tiles were scanned and found empty, hollow tiles are not yet crawled.",
    body
  );
}

// ---------- project health (bus-factor, governance files, scorecard) ----------
// DDS = developer distribution score (ecosyste.ms commit_stats): low means a
// repo's commits concentrate in few hands (bus-factor risk), high means they
// spread across many contributors. Red→green ramp encodes risk→healthy.
const DDS_COLORS = ["#f85149", "#db6d28", "#d29922", "#57ab5a", "#3fb950"];

function prettyKind(kind: string): string {
  const s = kind.replace(/_/g, " ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function healthView(h: Health): string {
  if (!h || h.enriched_stats <= 0) {
    return box(
      "Health",
      "Project health",
      "Bus-factor, governance-file and security metrics from ecosyste.ms enrichment.",
      `<div class="empty">No enrichment data yet — run an enrich pass to populate the bus-factor, governance-file and OSSF scorecard metrics.</div>`
    );
  }

  // Bus factor: DDS histogram, coloured risk→healthy.
  const maxDDS = Math.max(1, ...h.dds_buckets.map((b) => b.repos));
  const ddsRows = h.dds_buckets
    .map((b, i) => {
      const w = ((b.repos / maxDDS) * 100).toFixed(1);
      const label = `${b.lo.toFixed(1)}–${b.hi.toFixed(1)}`;
      return `
      <div class="lang-row" style="--w:${w}%; --i:${i}">
        <span class="lang-name">${label}</span>
        <span class="lang-bar"><span class="lang-fill" style="background:${DDS_COLORS[i] ?? "var(--success)"}"></span></span>
        <span class="lang-val"><b>${compact(b.repos)}</b> repos</span>
      </div>`;
    })
    .join("");

  // Community files: adoption rate per governance file, most-common first.
  const files = h.files.slice(0, 10);
  const fileRows = files
    .map((f, i) => {
      const w = (f.rate * 100).toFixed(1);
      return `
      <div class="lang-row" style="--w:${w}%; --i:${i}">
        <span class="lang-name">${esc(prettyKind(f.kind))}</span>
        <span class="lang-bar"><span class="lang-fill"></span></span>
        <span class="lang-val"><b>${(f.rate * 100).toFixed(0)}%</b> · ${compact(f.present)}</span>
      </div>`;
    })
    .join("");

  const scorecard =
    h.scorecard_count > 0
      ? `<div class="health-scorecard">
           <span class="hs-label">OSSF Scorecard</span>
           <span class="hs-score"><b>${h.scorecard_avg.toFixed(1)}</b> / 10</span>
           <span class="hs-sub">avg across ${grouped(h.scorecard_count)} repos</span>
         </div>`
      : "";

  const body = `
    <div class="health">
      <div class="health-block">
        <div class="health-head"><b>Bus factor</b><span>commit concentration (DDS) — left bands are few-hands / higher risk, right bands spread across many contributors</span></div>
        <div class="lang-chart">${ddsRows}</div>
      </div>
      <div class="health-block">
        <div class="health-head"><b>Community files</b><span>share of enriched repos carrying each governance file</span></div>
        <div class="lang-chart">${fileRows}</div>
      </div>
      ${scorecard}
    </div>`;
  return box(
    "Health",
    "Project health",
    `Bus-factor, governance-file adoption and OSSF security scores, from ${grouped(h.enriched_stats)} ecosyste.ms-enriched repositories.`,
    body
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
    // threshold 0 = reveal as soon as any pixel enters the viewport. A fixed
    // ratio like 0.1 silently breaks for sections taller than the viewport:
    // the Ranking section (40 rows) is ~4700px tall, so on first paint only
    // ~8% of it is on-screen — below 0.1 — and it stayed at opacity:0 until a
    // scroll pushed it past 10%, leaving the page blank on load.
    { threshold: 0 }
  );
  document.querySelectorAll(".section").forEach((s) => obs.observe(s));
}

async function main() {
  const app = document.getElementById("app")!;
  app.innerHTML = `<div class="loading">Loading survey…</div>`;
  try {
    const [meta, repos, yr, topics, langs, cov, health] = await Promise.all([
      getJSON<Meta>("meta.json"),
      getJSON<TopRepo[]>("top_repos.json"),
      getJSON<YearBucket[]>("trends.json"),
      getJSON<TopicCount[]>("topics.json"),
      getJSON<LanguageCount[]>("languages.json"),
      getJSON<Coverage>("coverage.json"),
      getJSON<Health>("health.json"),
    ]);
    app.insertAdjacentHTML("beforebegin", topbar());
    wireTheme();
    renderStarBadge();
    app.innerHTML =
      hero(meta) +
      sectionNav() +
      rankings(repos) +
      languages(langs) +
      trends(yr) +
      coverageView(cov) +
      healthView(health) +
      topicsView(topics) +
      footer(meta);
    wireRankings();
    wireTopics();
    wireNav();
    reveal();
  } catch (err) {
    app.innerHTML = `<div class="loading">Could not load the survey data.<br/>${esc(String(err))}</div>`;
  }
}

main();
