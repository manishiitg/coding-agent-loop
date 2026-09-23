// Activity previews need their own inline scripts for answer widgets. Keep
// external scripts blocked and the iframe origin isolated while also retaining
// its scroll position when Quill edits the page.
export function withPreviewPositionScript(html: string, savedY: number): string {
  const policy = `<meta http-equiv="Content-Security-Policy" content="script-src 'unsafe-inline'">`
  const secured = /<head\b[^>]*>/i.test(html)
    ? html.replace(/<head\b[^>]*>/i, (head) => head + policy)
    : /<html\b[^>]*>/i.test(html)
      ? html.replace(/<html\b[^>]*>/i, (root) => root + `<head>${policy}</head>`)
      : html.replace(/^(\s*<!doctype[^>]*>)/i, `$1<head>${policy}</head>`)
  return secured + `
<script>(function(){
  var savedY = ${Math.max(0, Math.round(savedY))};
  function restore(){ if (savedY > 0) window.scrollTo(0, savedY); }
  window.addEventListener('load', restore);
  setTimeout(restore, 60);
  var pending = false;
  window.addEventListener('scroll', function(){
    if (pending) return;
    pending = true;
    requestAnimationFrame(function(){
      pending = false;
      parent.postMessage({ __sq: 1, op: 'preview-scroll', y: window.scrollY }, '*');
    });
  }, { passive: true });
})();</script>`
}
