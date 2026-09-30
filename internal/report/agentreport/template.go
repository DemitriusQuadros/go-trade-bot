package agentreport

// reportTemplate is the self-contained Console Pro report document. Colour
// tokens are copied from web/src/index.css (light ":root" paper set and the
// ".dark" amber-on-near-black set) and fonts from web/tailwind.config.js
// (IBM Plex Sans for chrome, IBM Plex Mono for data/code). Dark mode applies
// via prefers-color-scheme unless the <html> element carries class="light",
// or is forced with class="dark" (the /html endpoint injects either for
// ?theme=). No external resources and no <script>: the report is served with
// a default-src 'none' CSP inside a sandboxed iframe.
//
// Static labels come from the Messages catalog via the per-render "t"
// (message), "tf" (formatted message) and "sev" (severity name) funcs; the
// document's lang is the render locale (i18n-02 §3).
//
// Keep the token values in sync with web/src/index.css by hand.
const reportTemplate = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
:root {
  --background: 41 47% 90%;
  --foreground: 35 19% 13%;
  --card: 42 62% 96%;
  --card-foreground: 35 19% 13%;
  --primary: 30 71% 39%;
  --primary-foreground: 0 0% 100%;
  --secondary: 41 42% 85%;
  --muted: 41 42% 85%;
  --muted-foreground: 39 14% 40%;
  --destructive: 3 58% 48%;
  --success: 149 71% 30%;
  --warning: 39 77% 31%;
  --border: 43 41% 77%;
  --radius: 0.5rem;
}
.dark {
  --background: 216 19% 5%;
  --foreground: 202 20% 92%;
  --card: 214 15% 9%;
  --card-foreground: 202 20% 92%;
  --primary: 30 70% 51%;
  --primary-foreground: 35 80% 6%;
  --secondary: 217 16% 16%;
  --muted: 217 16% 16%;
  --muted-foreground: 213 10% 59%;
  --destructive: 358 75% 59%;
  --success: 150 50% 50%;
  --warning: 46 73% 56%;
  --border: 217 16% 16%;
}
@media (prefers-color-scheme: dark) {
  :root:not(.light) {
    --background: 216 19% 5%;
    --foreground: 202 20% 92%;
    --card: 214 15% 9%;
    --card-foreground: 202 20% 92%;
    --primary: 30 70% 51%;
    --primary-foreground: 35 80% 6%;
    --secondary: 217 16% 16%;
    --muted: 217 16% 16%;
    --muted-foreground: 213 10% 59%;
    --destructive: 358 75% 59%;
    --success: 150 50% 50%;
    --warning: 46 73% 56%;
    --border: 217 16% 16%;
  }
}
* { box-sizing: border-box; border-color: hsl(var(--border)); }
html, body { margin: 0; padding: 0; }
body {
  background: hsl(var(--background));
  color: hsl(var(--foreground));
  font-family: "IBM Plex Sans", ui-sans-serif, system-ui, sans-serif;
  font-size: 14px;
  line-height: 1.55;
  -webkit-font-smoothing: antialiased;
}
.mono, code, pre, table.data td.num, .kpi .value, .chart-axis { font-family: "IBM Plex Mono", ui-monospace, monospace; }
.report { max-width: 960px; margin: 0 auto; padding: 24px 20px 32px; }
.report-header { border-bottom: 1px solid hsl(var(--border)); padding-bottom: 16px; margin-bottom: 20px; }
.eyebrow { font-size: 11px; letter-spacing: 0.08em; text-transform: uppercase; color: hsl(var(--muted-foreground)); }
h1 { font-size: 22px; font-weight: 600; margin: 4px 0 10px; }
h2 { font-size: 13px; font-weight: 600; margin: 0 0 10px; color: hsl(var(--muted-foreground)); text-transform: uppercase; letter-spacing: 0.06em; }
.meta { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; font-size: 12px; color: hsl(var(--muted-foreground)); }
.badge { display: inline-block; border-radius: 9999px; padding: 1px 8px; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; border: 1px solid transparent; }
.sev-info { color: hsl(var(--primary)); border-color: hsl(var(--primary) / 0.4); background: hsl(var(--primary) / 0.08); }
.sev-warning { color: hsl(var(--warning)); border-color: hsl(var(--warning) / 0.4); background: hsl(var(--warning) / 0.1); }
.sev-critical { color: hsl(var(--destructive)); border-color: hsl(var(--destructive) / 0.45); background: hsl(var(--destructive) / 0.1); }
.card { background: hsl(var(--card)); color: hsl(var(--card-foreground)); border: 1px solid hsl(var(--border)); border-radius: var(--radius); padding: 16px; margin-bottom: 16px; }
.source { font-size: 12px; color: hsl(var(--muted-foreground)); margin: -4px 0 12px; }
p { margin: 0 0 10px; white-space: pre-wrap; }
p:last-child { margin-bottom: 0; }
.callout { border-left-width: 3px; }
.callout.sev-info { border-left-color: hsl(var(--primary)); }
.callout.sev-warning { border-left-color: hsl(var(--warning)); }
.callout.sev-critical { border-left-color: hsl(var(--destructive)); }
.callout .callout-title { font-weight: 600; margin-bottom: 6px; }
.kpis { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 10px; }
.kpi { border: 1px solid hsl(var(--border)); border-radius: calc(var(--radius) - 2px); padding: 10px 12px; background: hsl(var(--background) / 0.5); }
.kpi .label { font-size: 11px; color: hsl(var(--muted-foreground)); text-transform: uppercase; letter-spacing: 0.05em; }
.kpi .value { font-size: 18px; font-weight: 600; margin-top: 2px; }
.pos { color: hsl(var(--success)); }
.neg { color: hsl(var(--destructive)); }
svg.chart { width: 100%; height: auto; display: block; }
svg.chart .grid { stroke: hsl(var(--border)); stroke-width: 1; }
svg.chart .line { fill: none; stroke-width: 1.75; }
svg.chart .line.pos { stroke: hsl(var(--success)); }
svg.chart .line.neg { stroke: hsl(var(--destructive)); }
svg.chart .area.pos { fill: hsl(var(--success) / 0.08); }
svg.chart .area.neg { fill: hsl(var(--destructive) / 0.08); }
svg.chart .chart-axis { fill: hsl(var(--muted-foreground)); font-size: 10px; }
.empty { color: hsl(var(--muted-foreground)); font-style: italic; }
.table-wrap { overflow-x: auto; }
table.data { width: 100%; border-collapse: collapse; font-size: 12px; }
table.data th { text-align: left; font-weight: 600; color: hsl(var(--muted-foreground)); border-bottom: 1px solid hsl(var(--border)); padding: 6px 8px; white-space: nowrap; }
table.data td { border-bottom: 1px solid hsl(var(--border) / 0.6); padding: 6px 8px; vertical-align: top; }
table.data td.num { text-align: right; white-space: nowrap; }
.note { font-size: 12px; color: hsl(var(--muted-foreground)); margin-top: 8px; }
pre.diff { margin: 0; padding: 8px 0; border-radius: calc(var(--radius) - 2px); background: hsl(var(--background) / 0.6); border: 1px solid hsl(var(--border)); overflow-x: auto; font-size: 12px; line-height: 1.5; }
pre.diff span { display: block; padding: 0 12px; white-space: pre; }
pre.diff .add { background: hsl(var(--success) / 0.12); color: hsl(var(--success)); }
pre.diff .del { background: hsl(var(--destructive) / 0.12); color: hsl(var(--destructive)); }
pre.diff .gap { color: hsl(var(--muted-foreground)); font-style: italic; }
ol.recs { margin: 0; padding-left: 20px; }
ol.recs li { margin-bottom: 10px; }
ol.recs .rec-title { font-weight: 600; }
ol.recs .rec-action { font-size: 12px; color: hsl(var(--primary)); margin-top: 2px; }
footer { margin-top: 24px; padding-top: 12px; border-top: 1px solid hsl(var(--border)); font-size: 12px; color: hsl(var(--muted-foreground)); }
</style>
</head>
<body>
<main class="report">
<header class="report-header">
<div class="eyebrow">{{t "report.eyebrow"}}</div>
<h1>{{.Title}}</h1>
<div class="meta">
<span class="badge sev-{{.Severity}}">{{sev .Severity}}</span>
<span>{{.AgentName}}</span>
<span>·</span>
<span class="mono">{{.CreatedAt}}</span>
{{- if .Strategies}}
<span>·</span>
<span>{{.Strategies}}</span>
{{- end}}
</div>
</header>
{{range .Blocks}}
{{- if eq .Type "summary"}}
<section class="card summary">
{{- range .Paragraphs}}
<p>{{.}}</p>
{{- end}}
</section>
{{- else if eq .Type "callout"}}
<section class="card callout sev-{{.Severity}}">
{{- if .Title}}
<div class="callout-title"><span class="badge sev-{{.Severity}}">{{sev .Severity}}</span> {{.Title}}</div>
{{- end}}
{{- range .Paragraphs}}
<p>{{.}}</p>
{{- end}}
</section>
{{- else if eq .Type "kpi_grid"}}
<section class="card">
<h2>{{t "heading.key_metrics"}}</h2>
<div class="source">{{.SourceLabel}}</div>
<div class="kpis">
{{- range .KPIs}}
<div class="kpi"><div class="label">{{.Label}}</div><div class="value {{.Tone}}">{{.Value}}</div></div>
{{- end}}
</div>
</section>
{{- else if eq .Type "equity_chart"}}
<section class="card">
<h2>{{t "heading.equity"}}</h2>
<div class="source">{{.SourceLabel}}</div>
{{- if .Chart.Empty}}
<div class="empty">{{t "equity.empty"}}</div>
{{- else}}
<svg class="chart" viewBox="0 0 640 220" role="img" aria-label="{{t "equity.aria"}}">
<line class="grid" x1="56" y1="18" x2="584" y2="18"></line>
<line class="grid" x1="56" y1="110" x2="584" y2="110"></line>
<line class="grid" x1="56" y1="202" x2="584" y2="202"></line>
<path class="area {{.Chart.Tone}}" d="{{.Chart.AreaPath}}"></path>
<path class="line {{.Chart.Tone}}" d="{{.Chart.Path}}"></path>
<text class="chart-axis" x="50" y="22" text-anchor="end">{{.Chart.MaxLabel}}</text>
<text class="chart-axis" x="50" y="206" text-anchor="end">{{.Chart.MinLabel}}</text>
<text class="chart-axis" x="56" y="216">{{.Chart.StartDate}}</text>
<text class="chart-axis" x="584" y="216" text-anchor="end">{{.Chart.EndDate}}</text>
</svg>
{{- end}}
</section>
{{- else if eq .Type "trade_table"}}
<section class="card">
<h2>{{t "heading.trades"}}</h2>
<div class="source">{{.SourceLabel}}</div>
{{- if .Trades}}
<div class="table-wrap">
<table class="data">
<thead><tr><th>{{t "trades.col.symbol"}}</th><th>{{t "trades.col.entry"}}</th><th>{{t "trades.col.exit"}}</th><th>{{t "trades.col.entry_px"}}</th><th>{{t "trades.col.exit_px"}}</th><th>{{t "trades.col.qty"}}</th><th>{{t "trades.col.pnl"}}</th><th>{{t "trades.col.reason"}}</th></tr></thead>
<tbody>
{{- range .Trades}}
<tr><td>{{.Symbol}}</td><td class="num">{{.Entry}}</td><td class="num">{{.Exit}}</td><td class="num">{{.EntryPx}}</td><td class="num">{{.ExitPx}}</td><td class="num">{{.Qty}}</td><td class="num {{.Tone}}">{{.Profit}}</td><td>{{.Reason}}</td></tr>
{{- end}}
</tbody>
</table>
</div>
{{- if .TradesNote}}
<div class="note">{{.TradesNote}}</div>
{{- end}}
{{- else}}
<div class="empty">{{t "trades.empty"}}</div>
{{- end}}
</section>
{{- else if eq .Type "code_diff"}}
<section class="card">
<h2>{{t "heading.script_changes"}}</h2>
<div class="source">{{.SourceLabel}}</div>
{{- if .Diff}}
<pre class="diff mono">
{{- range .Diff}}<span class="{{.Kind}}">{{.Sign}} {{.Text}}</span>{{end -}}
</pre>
{{- end}}
{{- if .DiffNote}}
<div class="note">{{.DiffNote}}</div>
{{- end}}
</section>
{{- else if eq .Type "recommendation"}}
<section class="card">
<h2>{{t "heading.recommendations"}}</h2>
<ol class="recs">
{{- range .Recs}}
<li><div class="rec-title">{{.Title}}</div>{{if .Rationale}}<div>{{.Rationale}}</div>{{end}}{{if .Action}}<div class="rec-action">{{.Action}}</div>{{end}}</li>
{{- end}}
</ol>
</section>
{{- else if eq .Type "text_table"}}
<section class="card">
<div class="table-wrap">
<table class="data">
<thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead>
<tbody>
{{- range .Rows}}
<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>
{{- end}}
</tbody>
</table>
</div>
</section>
{{- end}}
{{end}}
<footer>{{tf "footer.generated_by" .AgentName .RunID}}</footer>
</main>
</body>
</html>
`
