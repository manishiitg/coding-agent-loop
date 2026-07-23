## Starter HTML skeleton for `builder/improve.html`

Use this document only when creating a new `builder/improve.html` or doing the required one-time upgrade from an old-format Pulse/improve log. For log semantics, entry kinds, close-out rules, and migration triggers, first load `get_reference_doc(kind="review-improve-log")`.

### Starter HTML skeleton (copy this exactly)

`builder/improve.html` renders in a full sandboxed iframe — the same way reports render — so it supports real CSS, web fonts, and themes. There is no excuse for a plain or ugly log: match the polish below. When bootstrapping a new log, write this document verbatim, fill the header, and leave the `<!-- LOG ENTRIES: newest first -->` anchor in place. On every later turn, insert new entry cards **immediately after that anchor** (newest on top). Keep the CSS block stable so the look stays consistent run to run.

```html
<!doctype html>
<html lang="en" data-pulse-schema="2">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title><!-- WORKFLOW NAME --> · pulse</title>
<style>
  :root{
    --bg:#f7f7f5;--surface:#fff;--surface-2:#fbfbfa;
    --ink:#191917;--ink-2:#57564f;--ink-3:#8a897f;
    --line:#eceae4;--line-2:#e0ded7;
    --ok:#247a58;--ok-bg:#e4f7ed;--warn:#a45f00;--warn-bg:#fff0cf;--bad:#bd3445;--bad-bg:#ffe3e8;
    --goal:#7c4dd8;--goal-bg:#f0e9ff;--decision:#0d7584;--decision-bg:#e3f7f8;--major:#c43d79;--major-bg:#ffe4f0;--user:#2c70c9;--user-bg:#e7f0ff;--teal:#168477;--teal-bg:#dff7f2;--amber:#b65c00;--amber-bg:#fff0d6;
    --shadow:0 1px 2px rgba(20,20,18,.04),0 4px 16px -8px rgba(20,20,18,.10);
    --mono:"SF Mono",ui-monospace,"JetBrains Mono",Menlo,monospace;--sans:"Inter",-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;--r:14px;}
  /* Dark palette — the app injects data-theme="dark" on <html> when its theme is dark. Keep this block. */
  html[data-theme="dark"]{
    --bg:#0a0a0c;--surface:#15151a;--surface-2:#101014;
    --ink:#f1f0f4;--ink-2:#9b9ba6;--ink-3:#64646e;
    --line:#212128;--line-2:#2e2e37;
    --ok:#69dfa0;--ok-bg:#10291d;--warn:#f0ba59;--warn-bg:#2c210e;--bad:#ff8794;--bad-bg:#32151b;
    --goal:#c4a7ff;--goal-bg:#201632;--decision:#77d5e4;--decision-bg:#102a30;--major:#ff8abc;--major-bg:#321421;--user:#82b8ff;--user-bg:#10213b;--teal:#5ee4d2;--teal-bg:#0d2a27;--amber:#f5b45f;--amber-bg:#2d1f0c;
    --shadow:0 1px 0 rgba(255,255,255,.04) inset,0 1px 2px rgba(0,0,0,.45),0 10px 30px -14px rgba(0,0,0,.75);}
  html{color-scheme:light} html[data-theme="dark"]{color-scheme:dark}
  *{box-sizing:border-box}
  html,body{width:100%;max-width:100%;overflow-x:hidden}
  body{margin:0;background:var(--bg);color:var(--ink);font-family:var(--sans);font-size:14px;line-height:1.5;-webkit-font-smoothing:antialiased;font-feature-settings:"cv02","cv03","ss01";font-variant-numeric:tabular-nums;overflow-wrap:normal;word-break:normal}
  html[data-theme="dark"] body{background:radial-gradient(1100px 520px at 50% -8%, #17171e 0%, var(--bg) 58%) fixed}
  code,.status .txt,.briefitem p,.tile .d,.entry p,.entry .meta,.decisiongrid span,.arow,footer{overflow-wrap:anywhere}
  .wrap{width:100%;max-width:820px;margin:0 auto;padding:16px 12px 56px}
  .top{display:block}
  .eyebrow{font:600 11px/1 var(--mono);letter-spacing:.14em;color:var(--ink-3);text-transform:uppercase}
  h1{font-size:24px;line-height:1.08;letter-spacing:-.01em;margin:8px 0 0;font-weight:660}
  .verdicts{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px}
  .pill{display:inline-flex;align-items:center;gap:7px;font:650 12px/1 var(--sans);padding:8px 11px;border-radius:999px;border:1px solid transparent;max-width:100%;white-space:nowrap;overflow-wrap:normal;word-break:normal}
  .pill .lbl{font:700 8.5px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;opacity:.65}
  .pill .as{font:540 10px/1 var(--mono);opacity:.55;margin-left:1px}
  .pill.ok{background:var(--ok-bg);color:var(--ok);border-color:color-mix(in srgb,var(--ok) 16%,transparent)}
  .pill.warn{background:var(--warn-bg);color:var(--warn);border-color:color-mix(in srgb,var(--warn) 16%,transparent)}
  .pill.bad{background:var(--bad-bg);color:var(--bad);border-color:color-mix(in srgb,var(--bad) 18%,transparent)}
  .dot{width:7px;height:7px;border-radius:50%;background:currentColor;box-shadow:0 0 0 3px color-mix(in srgb,currentColor 18%,transparent)}
  /* Status headline — the 1-second read; mirrors the monitor's one-sentence verdict. */
  .status{display:flex;align-items:flex-start;gap:10px;flex-wrap:wrap;margin:18px 0 0;padding:13px 14px;border-radius:13px;border:1px solid var(--line-2);background:var(--surface);box-shadow:var(--shadow);font-size:14px;font-weight:560}
  .status .ic{flex:none;width:9px;height:9px;border-radius:50%;background:currentColor;box-shadow:0 0 0 4px color-mix(in srgb,currentColor 15%,transparent)}
  .status.ok{color:var(--ok)} .status.warn{color:var(--warn)} .status.bad{color:var(--bad)}
  .status .txt{color:var(--ink);font-weight:580;min-width:0;flex:1 1 220px}.status .when{margin-left:19px;flex-basis:100%;font:540 11px/1.35 var(--mono);color:var(--ink-3);white-space:normal}
  .chips{display:flex;flex-wrap:wrap;gap:7px;margin-top:16px}
  .chip{font:520 12px/1 var(--sans);padding:6px 11px;border-radius:8px;background:var(--surface);border:1px solid var(--line-2);color:var(--ink-2);white-space:nowrap;overflow-wrap:normal;word-break:normal} .chip b{color:var(--ink);font-weight:600}
  .brief{margin-top:16px;border:1px solid var(--line-2);border-radius:var(--r);background:linear-gradient(180deg,color-mix(in srgb,var(--surface-2) 72%,var(--surface)),var(--surface));box-shadow:var(--shadow);padding:14px}
  .brief-h{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:10px;font:700 10.5px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--ink-3)}
  .brief-h b{font:600 11px/1.2 var(--mono);letter-spacing:0;text-transform:none;color:var(--ink-2);white-space:nowrap}
  .briefgrid{display:grid;grid-template-columns:1fr;gap:9px}
  .briefitem{min-width:0;padding:10px 11px;border:1px solid var(--line);border-radius:10px;background:color-mix(in srgb,var(--surface) 86%,var(--surface-2))}
  .briefitem .k{font:700 9.5px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-3);margin-bottom:6px}
  .briefitem p{margin:0;font:540 13px/1.45 var(--sans);color:var(--ink)}
  .briefitem.ok{border-color:color-mix(in srgb,var(--ok) 18%,var(--line));background:color-mix(in srgb,var(--ok-bg) 22%,var(--surface))}
  .briefitem.warn{border-color:color-mix(in srgb,var(--warn) 20%,var(--line));background:color-mix(in srgb,var(--warn-bg) 26%,var(--surface))}
  .briefitem.bad{border-color:color-mix(in srgb,var(--bad) 20%,var(--line));background:color-mix(in srgb,var(--bad-bg) 24%,var(--surface))}
  .assumptions{margin-top:16px;border:1px solid color-mix(in srgb,var(--major) 28%,var(--line-2));border-radius:var(--r);background:linear-gradient(180deg,color-mix(in srgb,var(--major-bg) 40%,var(--surface)),var(--surface));box-shadow:var(--shadow);overflow:hidden}
  .assumptions .ah{padding:12px 14px;border-bottom:1px solid color-mix(in srgb,var(--major) 16%,var(--line));font:700 10.5px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--major)}
  .assumption{padding:12px 14px;border-top:1px solid var(--line)}.assumption:first-of-type{border-top:0}.assumption b{display:block;font-size:13.5px;line-height:1.4}.assumption p{margin:5px 0 0;color:var(--ink-2);font-size:12.5px;line-height:1.5}.assumption .source{font:540 10.5px/1.4 var(--mono);color:var(--ink-3)}
  .technical{margin-top:24px;border:1px solid var(--line-2);border-radius:var(--r);background:var(--surface);box-shadow:var(--shadow);overflow:hidden}
  .technical>summary{display:flex;align-items:center;justify-content:space-between;gap:12px;cursor:pointer;list-style:none;padding:13px 15px;font-weight:650;color:var(--ink)}.technical>summary::-webkit-details-marker{display:none}.technical>summary::after{content:"+";font:700 16px/1 var(--mono);color:var(--ink-3)}.technical[open]>summary::after{content:"−"}.technical>summary span{margin-left:auto;font:540 11px/1.35 var(--mono);color:var(--ink-3)}
  .technical .techbody{padding:0 14px 16px;border-top:1px solid var(--line)}.technical .grouplbl{margin-top:22px}
  .agentlog{margin-top:34px;border:1px solid var(--line-2);border-radius:12px;background:var(--surface-2);overflow:hidden}
  .agentlog>summary{display:flex;align-items:center;gap:10px;cursor:pointer;list-style:none;padding:12px 14px;font:650 12px/1.3 var(--mono);color:var(--ink-2)}.agentlog>summary::-webkit-details-marker{display:none}.agentlog>summary::after{content:"+";margin-left:auto;font:700 15px/1 var(--mono);color:var(--ink-3)}.agentlog[open]>summary::after{content:"−"}.agentlog>summary span{font:520 10.5px/1.35 var(--mono);color:var(--ink-3)}
  .agentbody{padding:10px 14px 13px;border-top:1px solid var(--line);display:grid;gap:8px}.agentrow{display:grid;gap:3px;font:520 11px/1.45 var(--mono);color:var(--ink-3)}.agentrow b{color:var(--ink-2);font-weight:650}.agentrow code{white-space:pre-wrap;overflow-wrap:anywhere;color:var(--ink-3)}
  .filters{display:grid;grid-template-columns:1fr;gap:9px;margin:28px 0 0;padding:12px;border:1px solid var(--line-2);border-radius:12px;background:var(--surface);box-shadow:var(--shadow)}
  .filters label{display:grid;gap:6px;font:700 9.5px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-3)}
  .filters input,.filters select{width:100%;min-height:34px;border:1px solid var(--line-2);border-radius:9px;background:var(--surface-2);color:var(--ink);font:540 13px/1.2 var(--sans);padding:7px 9px}
  .filters button{min-height:34px;border:1px solid var(--line-2);border-radius:9px;background:var(--surface-2);color:var(--ink-2);font:650 12px/1 var(--sans);padding:7px 11px;cursor:pointer}
  .filters button:hover{border-color:var(--ink-3);color:var(--ink)}
  .filtercount{align-self:end;font:600 11px/1.35 var(--mono);color:var(--ink-3)}
  .grouplbl{display:flex;align-items:center;gap:8px;font:650 11px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--ink-3);margin:30px 2px 12px} .grouplbl::after{content:"";flex:1;height:1px;background:var(--line)}
  .seclabel{font:650 11px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--ink-3);margin:34px 2px 14px}
  .tiles{display:grid;grid-template-columns:1fr;gap:10px}
  .tile{min-width:0;background:var(--surface);border:1px solid var(--line-2);border-radius:12px;padding:13px 14px;box-shadow:var(--shadow)}
  .tile.ok{border-color:color-mix(in srgb,var(--ok) 24%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--ok-bg) 40%,var(--surface)),var(--surface))}
  .tile.warn{border-color:color-mix(in srgb,var(--warn) 26%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--warn-bg) 42%,var(--surface)),var(--surface))}
  .tile.bad{border-color:color-mix(in srgb,var(--bad) 24%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--bad-bg) 40%,var(--surface)),var(--surface))}
  .tile.info{border-color:color-mix(in srgb,var(--user) 22%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--user-bg) 40%,var(--surface)),var(--surface))}
  .tile.goal{border-color:color-mix(in srgb,var(--goal) 24%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--goal-bg) 42%,var(--surface)),var(--surface))}
  .tile.cost{border-color:color-mix(in srgb,var(--amber) 24%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--amber-bg) 38%,var(--surface)),var(--surface))}
  .tile .k{font:600 10.5px/1 var(--mono);letter-spacing:.05em;text-transform:uppercase;color:var(--ink-3)}
  .tile .v{font-size:25px;font-weight:680;letter-spacing:-.02em;margin-top:10px;line-height:1} .tile .d{font:540 12px/1.3 var(--sans);margin-top:7px;color:var(--ink-2)} .tile .asof{font:540 10.5px/1.35 var(--mono);margin-top:8px;color:var(--ink-3)}
  .up{color:var(--ok)} .down{color:var(--bad)} .flat{color:var(--warn)}
  .runs{border:1px solid var(--line-2);border-radius:12px;overflow:hidden;background:var(--surface);box-shadow:var(--shadow)}
  .run{display:grid;grid-template-columns:1fr;gap:7px 10px;align-items:start;padding:12px 14px;border-top:1px solid var(--line);font:540 12px/1.35 var(--mono);color:var(--ink-2)}
  .run:first-child{border-top:none} .run.flag{background:color-mix(in srgb,var(--warn-bg) 60%,var(--surface))}.run[hidden],.entry[hidden]{display:none!important}
  .run .id{color:var(--ink);font-weight:680}.run .st{display:inline-flex;align-items:center;gap:6px}
  .run .st.ok{color:var(--ok)} .run .st.warn{color:var(--warn)} .run .st .d{width:5px;height:5px;border-radius:50%;background:currentColor}
  .run .id,.run .st,.run .col,.run .ago,.tag,.kind,.worklabel,.status .when,.ehead>.when{white-space:nowrap;overflow-wrap:normal;word-break:normal}
  .run .col b{color:var(--ink);font-weight:620}.run .note{grid-column:1/-1;color:var(--ink-2);font:560 12px/1.4 var(--sans);min-width:0;overflow-wrap:anywhere}.run.flag .note{color:var(--warn)}.run .ago{grid-column:1/-1;color:var(--ink-3)}
  .entry{position:relative;background:var(--surface);border:1px solid var(--line-2);border-radius:13px;padding:15px 14px 15px 18px;margin-bottom:12px;box-shadow:var(--shadow);min-width:0}
  .entry::before{content:"";position:absolute;left:0;top:14px;bottom:14px;width:3px;border-radius:3px;background:var(--line-2)}
  .entry.monitor::before{background:var(--warn)} .entry.maintenance::before{background:var(--teal)} .entry.agent::before{background:var(--ok)} .entry.decision::before{background:var(--decision)} .entry.decision.major::before{background:var(--major);width:4px} .entry.user::before{background:var(--user)} .entry.input::before{background:var(--user)} .entry.open::before{background:var(--bad)} .entry.note::before{background:var(--ink-3)}
  .entry.decision{border-color:color-mix(in srgb,var(--decision) 28%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--decision-bg) 46%,var(--surface)),var(--surface) 72%)}
  .entry.decision.major{border-color:color-mix(in srgb,var(--major) 38%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--major-bg) 62%,var(--surface)),var(--surface) 76%);box-shadow:0 0 0 1px color-mix(in srgb,var(--major) 15%,transparent),var(--shadow)}
  .ehead{display:flex;align-items:center;gap:7px;margin-bottom:8px;flex-wrap:wrap}
  .tag{font:700 9.5px/1 var(--mono);letter-spacing:.06em;text-transform:uppercase;padding:4px 8px;border-radius:6px}
  .tag.monitor{background:var(--warn-bg);color:var(--warn)} .tag.maintenance{background:var(--teal-bg);color:var(--teal)} .tag.agent{background:var(--ok-bg);color:var(--ok)} .tag.decision{background:var(--decision-bg);color:var(--decision);border:1px solid color-mix(in srgb,var(--decision) 22%,transparent)} .entry.major .tag.decision{background:var(--major-bg);color:var(--major);border-color:color-mix(in srgb,var(--major) 25%,transparent)} .tag.user,.tag.input{background:var(--user-bg);color:var(--user)} .tag.open{background:var(--bad-bg);color:var(--bad)} .tag.note{background:var(--surface-2);color:var(--ink-2);border:1px solid var(--line-2)}
  .kind{font:700 8.5px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;padding:4px 7px;border-radius:6px;border:1px solid}
  .kind.bug{color:var(--bad);border-color:color-mix(in srgb,var(--bad) 22%,transparent)} .kind.goal{color:var(--goal);border-color:color-mix(in srgb,var(--goal) 22%,transparent)}
  .worklabel{font:700 8.5px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;padding:4px 7px;border-radius:999px;background:var(--surface-2);border:1px solid var(--line-2);color:var(--ink-2)}
  .worklabel.bugfix{color:var(--bad);background:var(--bad-bg);border-color:color-mix(in srgb,var(--bad) 20%,transparent)} .worklabel.improvement{color:var(--goal);background:var(--goal-bg);border-color:color-mix(in srgb,var(--goal) 20%,transparent)} .worklabel.advisor{color:var(--major);background:var(--major-bg);border-color:color-mix(in srgb,var(--major) 22%,transparent)} .worklabel.artifact{color:var(--decision);background:var(--decision-bg);border-color:color-mix(in srgb,var(--decision) 20%,transparent)} .worklabel.report,.worklabel.eval{color:var(--warn);background:var(--warn-bg);border-color:color-mix(in srgb,var(--warn) 20%,transparent)} .worklabel.cost{color:var(--ink-2);background:var(--surface-2);border-color:var(--line-2)} .worklabel.maintenance{color:var(--teal);background:var(--teal-bg);border-color:color-mix(in srgb,var(--teal) 20%,transparent)} .worklabel.backup{color:var(--ok);background:var(--ok-bg);border-color:color-mix(in srgb,var(--ok) 20%,transparent)} .worklabel.input,.worklabel.manual{color:var(--user);background:var(--user-bg);border-color:color-mix(in srgb,var(--user) 20%,transparent)}
  .etitle{font-weight:630;font-size:14px;line-height:1.25;letter-spacing:-.01em;flex:1 1 auto;min-width:0}.ehead>.when{margin-left:0;flex-basis:100%;font:540 11px/1.35 var(--mono);color:var(--ink-3)}
  .entry p{margin:0;font-size:13.5px;color:var(--ink)}.entry p+p{margin-top:8px}.entry .takeaway{font-weight:720;color:var(--ink);font-size:14px;line-height:1.45}
  .entry .meta{margin-top:11px;padding-top:11px;border-top:1px solid var(--line);font:540 12px/1.5 var(--mono);color:var(--ink-3)} .entry .meta code{background:var(--surface-2);border:1px solid var(--line);border-radius:5px;padding:1px 6px;color:var(--ink-2)}
  .decisiongrid{display:grid;grid-template-columns:1fr;gap:8px;margin-top:11px}.decisiongrid>div{padding:9px 10px;border:1px solid color-mix(in srgb,var(--decision) 15%,var(--line));border-radius:10px;background:color-mix(in srgb,var(--surface) 88%,var(--decision-bg))}.entry.major .decisiongrid>div{border-color:color-mix(in srgb,var(--major) 18%,var(--line));background:color-mix(in srgb,var(--surface) 86%,var(--major-bg))}.decisiongrid b{display:block;margin-bottom:4px;font:700 9.5px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-3)}.decisiongrid span{display:block;color:var(--ink);font-size:13px;line-height:1.4}
  .resolved{margin-top:11px;display:inline-flex;align-items:center;gap:7px;font:620 12.5px/1.4 var(--sans);color:var(--ok)} .resolved::before{content:"✓";font-size:11px;width:16px;height:16px;display:inline-flex;align-items:center;justify-content:center;border-radius:50%;background:var(--ok-bg)}
  /* Outcome stamp on a Decision card — did the change actually move the number, judged by a later run. */
  .outcome{position:relative;margin-top:11px;display:block;padding-left:23px;font:600 12.5px/1.45 var(--sans)}
  .outcome::before{position:absolute;left:0;top:1px;font-size:11px;width:16px;height:16px;display:inline-flex;align-items:center;justify-content:center;border-radius:50%}
  .outcome.ok{color:var(--ok)} .outcome.ok::before{content:"✓";background:var(--ok-bg)}
  .outcome.bad{color:var(--bad)} .outcome.bad::before{content:"✗";background:var(--bad-bg)}
  .outcome.flat{color:var(--warn)} .outcome.flat::before{content:"–";background:var(--warn-bg)}
  .archive{border:1px solid var(--line-2);border-radius:12px;background:var(--surface);overflow:hidden;box-shadow:var(--shadow)}
  .arow{display:block;padding:13px 14px;border-top:1px solid var(--line);font-size:13.5px;color:var(--ink-2)} .arow:first-child{border-top:none} .arow b{color:var(--ink);font-weight:620} .arow .n{display:block;margin-top:4px;font:540 11px/1.35 var(--mono);color:var(--ink-3)}
  footer{margin-top:42px;padding-top:18px;border-top:1px solid var(--line);font:540 11.5px/1.5 var(--mono);color:var(--ink-3)}
  @media (min-width:640px){
    body{font-size:15px}
    .wrap{padding:28px 26px 88px}
    .top{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;flex-wrap:wrap}
    h1{font-size:31px;line-height:1.05;letter-spacing:-.025em}
    .verdicts{margin-top:0}.pill{font-size:13px;padding:9px 14px 9px 12px}
    .status{align-items:center;gap:12px;margin-top:22px;padding:15px 19px;font-size:15.5px}.status .txt{flex:1 1 auto}.status .when{margin-left:auto;flex-basis:auto;white-space:nowrap;font-size:12px}
    .brief{padding:16px}.briefgrid{grid-template-columns:repeat(2,minmax(0,1fr))}
    .filters{grid-template-columns:150px minmax(160px,1fr) auto auto;align-items:end;padding:13px 14px}.filtercount{justify-self:end;white-space:nowrap}
    .tiles{grid-template-columns:repeat(2,minmax(0,1fr))}.tile{padding:15px 16px}
    .run{display:grid;grid-template-columns:auto auto auto minmax(0,1fr) auto;gap:8px 14px;align-items:center;padding:12px 16px;font-size:13px;line-height:1.25}.run .id{grid-column:1;grid-row:1;min-width:44px}.run .st{grid-column:2;grid-row:1}.run .col{grid-row:1;min-width:78px}.run .note{grid-column:1/-1;grid-row:2;margin-top:4px;font-size:13px;line-height:1.45}.run .ago{grid-column:5;grid-row:1;justify-self:end;margin-left:0}
    .entry{padding:17px 19px 17px 22px}.etitle{font-size:15px}.ehead>.when{margin-left:auto;flex-basis:auto;white-space:nowrap;font-size:12px}.entry p{font-size:14.5px}
    .decisiongrid{grid-template-columns:repeat(2,minmax(0,1fr))}.decisiongrid span{font-size:13.5px}
    .arow{display:flex;gap:13px;align-items:center;padding:14px 18px;font-size:14px}.arow .n{display:block;margin-left:auto;margin-top:0;font-size:12px}
  }
</style>
</head>
<body><div class="wrap">

  <div class="top">
    <div><div class="eyebrow">workflow · pulse</div><h1><!-- WORKFLOW NAME --></h1></div>
    <div class="verdicts">
      <div id="pulse-bug-verdict" class="pill warn"><span class="lbl">Bug</span><span class="dot"></span>Not measured<span class="as">no run yet</span></div>
      <div id="pulse-goal-verdict" class="pill warn"><span class="lbl">Goal</span><span class="dot"></span>Not measured<span class="as">no run yet</span></div>
    </div>
  </div>

  <!-- VERDICTS — Gate updates these two stable elements in place on every Pulse.
       Bug: Clean | Warning | Broken. Goal: On target | Short | At risk | Not measured.
       Keep the .as freshness text visible as "run/date" or "not measured this run · last measured run/date".
       Use pill class ok|warn|bad. Never add a second verdict block elsewhere on the first screen. -->

  <!-- STATUS HEADLINE — the 1-second read. ONE compact plain-language sentence.
       Do not duplicate the full Bug/Goal verdict or latest run narrative here;
       detailed evidence belongs in Recent runs and timeline entries. Class ok|warn|bad
       tracks the current overall state. On a clean, on-target run say so plainly;
       don't manufacture concern. -->
  <div class="status ok">
    <span class="ic"></span>
    <span class="txt"><!-- e.g. Healthy and on-target. Latest clean run delivered; main open risk is exit geometry. --></span>
    <span class="when"><!-- run #— · — ago --></span>
  </div>

  <div class="chips">
    <span class="chip">Type <b><!-- primary type --></b></span>
    <span class="chip">Oversight <b><!-- oversight_mode --></b></span>
    <span class="chip">Last run <b>—</b></span>
  </div>

  <!-- ACTIVE ASSUMPTIONS CHALLENGED — render this block ONLY when a consequential assumption is actively limiting the workflow.
       Keep at most 3. An assumption is not an explicit user constraint. Remove resolved items from the top and record the outcome in the timeline.
  <div class="assumptions">
    <div class="ah">Assumptions challenged</div>
    <div class="assumption"><b>Assumption stated in plain language</b><p>Evidence for/against it and how it will be validated or retired.</p><p class="source">Came from: plan/step/eval/KB/learnings/report · not user-confirmed</p></div>
  </div>
  -->

  <!-- TODAY'S OUTCOME — four short operator-summary cells. Keep this brief; details belong in Recent runs/timeline.
       The heading says which run/date this summary reflects. If a cell carries an older metric, end its sentence with
       "not measured this run · last measured run/date" instead of making the older value look current. -->
  <div class="brief">
    <div class="brief-h">Today's outcome <b><!-- as of run #— --></b></div>
    <div class="briefgrid">
      <div class="briefitem ok"><div class="k">Outcome</div><p><!-- what the workflow actually delivered --></p></div>
      <div class="briefitem"><div class="k">Goal progress</div><p><!-- movement against success criteria --></p></div>
      <div class="briefitem warn"><div class="k">Issues &amp; fixes</div><p><!-- important issue/fix, or "No important issue" --></p></div>
      <div class="briefitem"><div class="k">Next Pulse</div><p><!-- what will be checked next and why --></p></div>
    </div>
  </div>

  <details class="technical">
    <summary>Technical details <span>signals · cost · maintenance</span></summary>
    <div class="techbody">

  <!-- SIGNAL TILES grouped by verdict. Read every number from eval reports,
       run outputs/logs, costs/, and timing summaries. Never invent. Every important tile needs a visible .asof line.
       When the latest route did not measure the signal, retain the last trustworthy value and write:
       <div class="asof">not measured this run · last measured run #— · YYYY-MM-DD</div> -->
  <div class="grouplbl">Bug · operational health</div>
  <div class="tiles">
    <div class="tile ok"><div class="k">Run status</div><div class="v">—</div><div class="d">no runs yet</div><div class="asof">as of run — · YYYY-MM-DD</div></div>
  </div>
  <div class="grouplbl">Goal · success criteria</div>
  <div class="tiles">
    <div class="tile goal"><div class="k">Goal signal</div><div class="v">—</div><div class="d">no runs yet</div><div class="asof">last measured run — · YYYY-MM-DD</div></div>
  </div>
  <div class="grouplbl">Cost + time · latest run</div>
  <div class="tiles">
    <div class="tile cost"><div class="k">Cost</div><div class="v">—</div><div class="d">missing cost evidence</div><div class="asof">as of run — · YYYY-MM-DD</div></div>
    <div class="tile info"><div class="k">Time</div><div class="v">—</div><div class="d">missing timing evidence</div><div class="asof">as of run — · YYYY-MM-DD</div></div>
    <!-- Keep this section compact. Good tile examples:
         Cost: "$0.27" / "1.2M tokens · top: score-companies $0.18"
         Time: "4m12s" / "LLM 2m08s · tools 51s · slowest: browser-agent 1m22s"
         Model mix: "high: opus · medium: sonnet" / "observed: claude-sonnet-4-6"
         Evidence: "costs/execution/group/date.json · runs/<run>/logs/<step>/execution/timing.json" -->
  </div>

  <div class="grouplbl">Maintenance radar · pulse depth</div>
  <div class="tiles">
    <div class="tile info"><div class="k">Pulse depth</div><div class="v">normal</div><div class="d">why this run was minimal, normal, or deep</div></div>
    <div class="tile info"><div class="k">Hygiene watch</div><div class="v">—</div><div class="d">learnings, KB, DB/report, publish/notify, model tiers</div></div>
    <!-- Keep this section explainable. Good examples:
         Pulse depth: "minimal" / "hourly run; no changelog, no new Bug, no answered input"
         Hygiene watch: "report dashboard" / "cost strip missing overhead; deep check next run" -->
  </div>

    </div>
  </details>

  <div class="filters" aria-label="Activity filters">
    <label>Kind <select id="filter-kind">
      <option value="all">All</option>
      <option value="run">Run</option>
      <option value="monitor">Monitor</option>
      <option value="maintenance">Maintenance</option>
      <option value="artifact">Artifact</option>
      <option value="decision">Decision</option>
      <option value="advisor">Advisor</option>
      <option value="cos">Chief of Staff</option>
      <option value="input">Question + answer</option>
      <option value="open">Open finding</option>
      <option value="user">User rule</option>
      <option value="note">Note</option>
    </select></label>
    <label>Search <input id="filter-search" type="search" placeholder="Text in runs or entries"></label>
    <button id="filter-clear" type="button">Reset</button>
    <div id="filter-count" class="filtercount">0 items</div>
  </div>

  <div class="seclabel">Recent runs</div>
  <div class="runs"><!-- one .run row per recent run. Metadata stays in row 1; the prose/evidence .note is row 2/full width.
       Example:
       <div class="run flag" data-date="2026-07-04" data-kind="run" data-pulse-section="reflection" data-module="run_summary"><span class="id">07-04</span><span class="st warn"><span class="d"></span>completed</span><span class="col">measure</span><span class="col"><b>Δ7d</b> -2</span><span class="ago">just now</span><span class="note">measure ran clean; regression still open; cost $2.02; backed up ✓ 3b1b357</span></div> --></div>

  <div class="seclabel">Latest — newest first</div>
  <!-- LOG ENTRIES: newest first -->
  <!-- Insert each new entry card immediately below this anchor. Monitor/Open-finding/Decision/Artifact Review carry a
       <span class="kind bug">Bug</span> or <span class="kind goal">Goal</span> verdict chip when applicable, plus a
       <span class="worklabel bugfix">Bug fix</span>, <span class="worklabel improvement">Improvement</span>, <span class="worklabel advisor">Advisor idea</span>, <span class="worklabel artifact">Artifact drift</span>, <span class="worklabel report">Report fix</span>, <span class="worklabel eval">Eval fix</span>, <span class="worklabel cost">Cost/time</span>, <span class="worklabel maintenance">Maintenance</span>, <span class="worklabel backup">Backup/publish</span>, <span class="worklabel input">Needs input</span>, or <span class="worklabel manual">Manual</span> action chip when work was done/proposed. Card kinds:
       <div class="entry monitor" data-date="YYYY-MM-DD" data-kind="monitor" data-pulse-section="signals" data-module="bug_review"><div class="ehead"><span class="tag monitor">Bug Review</span><span class="kind bug">Needs attention</span><span class="etitle">…</span><span class="when">…</span></div><p class="takeaway"><b>What happened:</b> Plain-language outcome.</p><p class="impact"><b>Why it matters:</b> Plain-language user impact.</p><p class="meta"><b>Next:</b> Plain-language next step.</p></div>
       <div class="entry maintenance" data-date="YYYY-MM-DD" data-kind="maintenance" data-pulse-section="reflection" data-module="run_summary"><div class="ehead"><span class="tag maintenance">Maintenance Radar</span><span class="worklabel maintenance">Maintenance</span><span class="etitle">Pulse depth: minimal|normal|deep</span><span class="when">…</span></div><p class="takeaway">Plain-language reason this run did or skipped optional maintenance.</p><p><b>Radar:</b> learnings · KB · DB/report · publish/notify · model/tier.</p></div>
       <div class="entry agent" data-date="YYYY-MM-DD" data-kind="decision" data-pulse-section="improvements" data-module="pulse_fixer"><div class="ehead"><span class="tag agent">Agent · fixed</span><span class="kind bug">Bug</span><span class="worklabel bugfix">Bug fix</span><span class="etitle">…</span><span class="when">…</span></div><p class="takeaway">Plain-language fix summary first.</p><p class="resolved">Resolved YYYY-MM-DD — how.</p></div>
       <div class="entry decision major" data-date="YYYY-MM-DD" data-kind="decision" data-pulse-section="improvements" data-module="goal_advisor"><div class="ehead"><span class="tag decision">Decision - Goal Advisor - Applied</span><span class="kind goal">Goal</span><span class="worklabel improvement">Improvement</span><span class="etitle">…</span><span class="when">…</span></div><p class="takeaway">Plain-language decision summary first.</p><div class="decisiongrid"><div><b>Why now</b><span>Plain business evidence.</span></div><div><b>Change</b><span>…</span></div><div><b>Expected impact</b><span>…</span></div><div><b>Risk / gap</b><span>…</span></div></div></div>
       <div class="entry decision major advisor-experiment" data-date="YYYY-MM-DD" data-kind="advisor" data-pulse-section="improvements" data-module="goal_advisor" data-experiment-kind="strategy" data-advisor-experiment-id="advisor-exp-<stable-slug>" data-status="proposed"><div class="ehead"><span class="tag decision">Decision - Goal Advisor - Proposed</span><span class="kind goal">Goal</span><span class="worklabel advisor">Advisor idea</span><span class="etitle">…</span><span class="when">…</span></div><p class="takeaway">Plain-language advisor idea first.</p><div class="decisiongrid"><div><b>Why now</b><span>Plain business evidence.</span></div><div><b>Change</b><span>Proposal only — out-of-plan idea and next decision.</span></div><div><b>Expected impact</b><span>…</span></div><div><b>Risk / gap</b><span>…</span></div></div></div>
       <div class="entry open" id="of-YYYY-MM-DD-slug" data-date="YYYY-MM-DD" data-kind="open" data-pulse-section="signals" data-module="bug_review"><div class="ehead"><span class="tag open">Open finding</span><span class="kind bug">Needs attention</span><span class="etitle">…</span><span class="when">…</span></div><p class="takeaway"><b>What happened:</b> Plain-language problem.</p><p class="impact"><b>Why it matters:</b> Plain-language user impact.</p><p class="meta"><b>Next:</b> Plain-language next step.</p></div>
       Pending questions stay only in db/db.sqlite and Runloop's source-owned Needs your decision surface. After an answer, add one historical card under whoever asked it. Goal Advisor example:
       <div class="entry input" data-date="YYYY-MM-DD" data-kind="input" data-pulse-section="improvements" data-module="goal_advisor" data-question-id="input-YYYY-MM-DD-slug" data-status="answered"><div class="ehead"><span class="tag input">Goal Advisor · user answer</span><span class="etitle">…</span><span class="when">…</span></div><div class="decisiongrid"><div><b>Question</b><span>…</span></div><div><b>Answer</b><span>selected option and/or free-form text</span></div><div><b>Outcome</b><span>Plain-language current outcome.</span></div></div></div>
       <div class="entry user" data-date="YYYY-MM-DD" data-kind="user" data-pulse-section="reflection" data-module="run_summary"><div class="ehead"><span class="tag user">User rule · authoritative</span><span class="etitle">…</span><span class="when">…</span></div><p class="takeaway">Plain-language rule first.</p></div>
       <div class="entry note" data-date="YYYY-MM-DD" data-kind="note" data-pulse-section="reflection" data-module="run_summary"><div class="ehead"><span class="tag note">Note</span><span class="etitle">…</span><span class="when">…</span></div><p class="takeaway">Plain-language note first.</p></div>
       Close an open finding by editing its card to add: <p class="resolved">Resolved YYYY-MM-DD — how.</p>
       Confirm a Decision worked (or didn't) by editing its card to add ONE outcome stamp once a later run measures it:
       <p class="outcome ok">Confirmed on YYYY-MM-DD — login-skip gone, eval 0.72 → 0.81 over 2 runs.</p>
       <p class="outcome bad">No effect by YYYY-MM-DD — reopened as a Goal finding.</p>
       <p class="outcome flat">Inconclusive on YYYY-MM-DD — the changed path was not exercised; still pending. -->

  <!-- HIDDEN RECOVERY HANDOFF — minimal interrupted-fix state only; technical proof stays in reviewer result files and SQLite. -->
  <div id="pulse-agent-handoff" data-pulse-run-id="" data-workflow-version="" hidden>
      <div class="agentrow" data-agent-key="state"><b>Current state</b><code>Latest execution: no result recorded yet</code></div>
      <!-- One compact row per module; use data attributes for scheduler-relevant continuity:
      <div class="agentrow" data-module="report_health" data-result="skipped" data-next-check-after-run-id="run-43"><b>report_health</b><code>evidence: builder/improve.html#of-report-gap</code></div>
      -->
      <div class="agentrow" data-agent-key="cursors"><b>Cursors and open ids</b><code>artifact=— · findings=— · inputs=— · cos=—</code></div>
  </div>

  <div class="seclabel">Archive</div>
  <div class="archive"><!-- one link per monthly archive file once history rolls off:
    <a class="arow" href="improve-archive/YYYY-MM.html"><b>YYYY-MM</b><span class="n">date range · N items</span></a>
    Keep one row per month; the Runloop Pulse viewer opens these links through the workspace archive endpoint. --></div>

  <footer>generated by the workflow agent · newest first · bug + goal verdicts · maintenance radar · archived monthly</footer>

<script>
(function(){
  var kindInput = document.getElementById('filter-kind');
  var searchInput = document.getElementById('filter-search');
  var clearButton = document.getElementById('filter-clear');
  var count = document.getElementById('filter-count');
  function norm(value){ return (value || '').toString().trim().toLowerCase(); }
  function items(){ return Array.prototype.slice.call(document.querySelectorAll('.run[data-date], .entry[data-date]')); }
  function apply(){
    var kind = kindInput ? kindInput.value : 'all';
    var query = norm(searchInput ? searchInput.value : '');
    var total = 0;
    var shown = 0;
    items().forEach(function(el){
      total += 1;
      var okKind = kind === 'all' || el.getAttribute('data-kind') === kind;
      var okText = !query || norm(el.textContent).indexOf(query) !== -1;
      var ok = okKind && okText;
      el.hidden = !ok;
      if (ok) shown += 1;
    });
    if (count) count.textContent = (kind !== 'all' || query) ? (shown + ' / ' + total + ' shown') : (total + ' items');
  }
  [kindInput, searchInput].forEach(function(el){ if (el) el.addEventListener('input', apply); });
  if (clearButton) clearButton.addEventListener('click', function(){
    if (kindInput) kindInput.value = 'all';
    if (searchInput) searchInput.value = '';
    apply();
  });
  apply();
})();
</script>
</div></body>
</html>
```
