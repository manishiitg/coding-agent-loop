export function buildPulseTimelineHtml(content: string): string {
  const script = `
<style id="__runloop_pulse_section_style">
  .runloop-section-host{width:100%;max-width:none;margin:0;padding:0}
  .runloop-section-host>.run,.runloop-section-host>.entry,.runloop-section-host>.pulse-record{width:100%;max-width:none}
  .runloop-section-empty{padding:28px 18px;text-align:center;color:var(--ink-3,#777);font:500 13px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
  body>.wrap{width:100%!important;max-width:none!important;padding:14px 12px 36px!important}
</style>
<script id="__runloop_pulse_section_script">
(function(){
  function text(el){ return (el.textContent || '').toLowerCase(); }
  function moduleFor(el){
    var explicit = el.getAttribute('data-module');
    if (explicit) return explicit;
    var value = text(el);
    var kind = el.getAttribute('data-kind') || '';
    if (kind === 'run' || kind === 'maintenance') return 'run_summary';
    if (value.indexOf('goal advisor') !== -1 || kind === 'advisor') return 'goal_advisor';
    if (kind === 'decision') return 'pulse_fixer';
    if (value.indexOf('artifact') !== -1 || value.indexOf('changelog') !== -1) return 'artifact_review';
    if (value.indexOf('learning') !== -1 || value.indexOf('skill.md') !== -1) return 'learning_health';
    if (value.indexOf('database') !== -1 || value.indexOf('db.sqlite') !== -1 || value.indexOf('db health') !== -1) return 'db_health';
    if (value.indexOf('cost') !== -1 || value.indexOf('token') !== -1 || value.indexOf('spend') !== -1) return 'cost_llm_time';
    if (value.indexOf('model') !== -1 || value.indexOf('llm') !== -1) return 'llm_ops_review';
    if (value.indexOf('evaluation') !== -1 || value.indexOf('eval ') !== -1) return 'eval_health';
    if (value.indexOf('report') !== -1 || value.indexOf('dashboard') !== -1) return 'report_health';
    if (value.indexOf('knowledge') !== -1) return 'knowledgebase_health';
    return '';
  }
  function render(target){
    if (!target) return;
    var wrap = document.querySelector('.wrap') || document.body;
    var candidates = Array.prototype.slice.call(document.querySelectorAll('.pulse-record,.run,.entry')).filter(function(el){
      return !el.parentElement || !el.parentElement.closest('.pulse-record,.run,.entry');
    });
    var matches = candidates.filter(function(el){ return moduleFor(el) === target; });
    matches.sort(function(a,b){
      return (b.getAttribute('data-date') || '').localeCompare(a.getAttribute('data-date') || '');
    });
    var host = document.getElementById('__runloop_pulse_section_host');
    if (!host) {
      Array.prototype.slice.call(wrap.children).forEach(function(child){ child.style.display = 'none'; });
      host = document.createElement('div');
      host.id = '__runloop_pulse_section_host';
      host.className = 'runloop-section-host timeline runs';
      wrap.appendChild(host);
    }
    host.replaceChildren();
    host.style.display = 'block';
    if (!matches.length) {
      var empty = document.createElement('div');
      empty.className = 'runloop-section-empty';
      var hasUnclassified = candidates.some(function(el){ return !moduleFor(el); });
      empty.textContent = hasUnclassified
        ? 'No classified history for this review yet. Older entries will be classified by the next Pulse upgrade.'
        : 'No history has been recorded for this review yet.';
      host.appendChild(empty);
    } else {
      matches.forEach(function(el){
        var clone = el.cloneNode(true);
        clone.removeAttribute('id');
        Array.prototype.forEach.call(clone.querySelectorAll('[id]'), function(node){ node.removeAttribute('id'); });
        clone.hidden = false;
        clone.style.display = '';
        host.appendChild(clone);
      });
    }
  }
  window.__runloopRenderPulseModule = render;
})();
</script>`

  if (/<\/body>/i.test(content)) return content.replace(/<\/body>/i, `${script}</body>`)
  return `${content}${script}`
}
