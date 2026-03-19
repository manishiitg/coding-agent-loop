const fs = require('fs');
const path = require('path');

const DEFAULT_URL = 'http://0.250.250.254:9222';
const cdpUrl = process.argv[2] || DEFAULT_URL;
const outputPath = path.join('runs', 'iteration-23', 'manish', 'execution', 'step-1', 'connection_test.json');

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function httpJson(url, options = {}) {
  const response = await fetch(url, options);
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${text}`);
  }
  return JSON.parse(text);
}

async function writeResult(result) {
  fs.writeFileSync(outputPath, JSON.stringify(result, null, 2));
}

async function main() {
  const result = {
    connected: false,
    twitter_visible: false,
    cdp_url: cdpUrl,
    error: null
  };

  let ws;
  try {
    const version = await httpJson(`${cdpUrl}/json/version`);
    if (!version.webSocketDebuggerUrl) {
      throw new Error('CDP version response missing webSocketDebuggerUrl');
    }
    result.connected = true;

    const target = await httpJson(`${cdpUrl}/json/new?https://x.com/home`, { method: 'PUT' });
    if (!target.webSocketDebuggerUrl) {
      throw new Error('CDP target response missing webSocketDebuggerUrl');
    }

    ws = new WebSocket(target.webSocketDebuggerUrl);
    let nextId = 1;
    const pending = new Map();
    let loadResolve = null;

    const waitForLoad = new Promise((resolve) => {
      loadResolve = resolve;
    });

    ws.addEventListener('message', (event) => {
      const message = JSON.parse(event.data.toString());
      if (message.id && pending.has(message.id)) {
        const { resolve, reject } = pending.get(message.id);
        pending.delete(message.id);
        if (message.error) {
          reject(new Error(JSON.stringify(message.error)));
        } else {
          resolve(message.result || {});
        }
        return;
      }
      if (message.method === 'Page.loadEventFired' && loadResolve) {
        loadResolve();
        loadResolve = null;
      }
    });

    await new Promise((resolve, reject) => {
      ws.addEventListener('open', resolve, { once: true });
      ws.addEventListener('error', reject, { once: true });
    });

    function send(method, params = {}) {
      return new Promise((resolve, reject) => {
        const id = nextId++;
        pending.set(id, { resolve, reject });
        ws.send(JSON.stringify({ id, method, params }));
      });
    }

    await send('Page.enable');
    await send('Runtime.enable');
    await send('Network.enable');
    await send('Network.setUserAgentOverride', {
      userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
      acceptLanguage: 'en-US,en;q=0.9',
      platform: 'MacIntel',
      userAgentMetadata: {
        brands: [
          { brand: 'Chromium', version: '131' },
          { brand: 'Google Chrome', version: '131' },
          { brand: 'Not_A Brand', version: '24' }
        ],
        fullVersion: '131.0.0.0',
        platform: 'macOS',
        platformVersion: '10.15.7',
        architecture: 'x86',
        model: '',
        mobile: false
      }
    });
    await send('Network.setExtraHTTPHeaders', {
      headers: {
        'Accept-Language': 'en-US,en;q=0.9',
        'sec-ch-ua': '"Chromium";v="131", "Google Chrome";v="131", "Not_A Brand";v="24"',
        'sec-ch-ua-mobile': '?0',
        'sec-ch-ua-platform': '"macOS"'
      }
    });

    await send('Page.navigate', { url: 'https://x.com/home' });
    await Promise.race([waitForLoad, delay(20000)]);
    await delay(5000);

    const evalResult = await send('Runtime.evaluate', {
      expression: `(() => {
        const bodyText = document.body ? document.body.innerText : '';
        const hasMain = !!document.querySelector('main[role="main"], [data-testid="primaryColumn"], [aria-label="Timeline: Your Home Timeline"]');
        const markers = [
          'Home',
          'For you',
          'Following',
          'What\\'s happening',
          'Post',
          'Timeline'
        ];
        const markerHits = markers.filter((marker) => bodyText.includes(marker));
        return {
          url: location.href,
          title: document.title,
          bodyText: bodyText.slice(0, 5000),
          hasMain,
          markerHits
        };
      })()`,
      returnByValue: true
    });

    const value = evalResult.result ? evalResult.result.value : null;
    const bodyText = value && typeof value.bodyText === 'string' ? value.bodyText : '';
    const markerHits = value && Array.isArray(value.markerHits) ? value.markerHits : [];
    const url = value && typeof value.url === 'string' ? value.url : '';
    const title = value && typeof value.title === 'string' ? value.title : '';
    const hasMain = !!(value && value.hasMain);

    result.twitter_visible =
      url.includes('x.com') &&
      (hasMain || markerHits.length > 0 || /home|timeline|post|follow|happening/i.test(`${title}\n${bodyText}`));

    if (!result.twitter_visible) {
      result.error = `Twitter UI markers not detected. url=${url} title=${title}`;
    }
  } catch (error) {
    result.error = error instanceof Error ? error.message : String(error);
  } finally {
    try {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.close();
      }
    } catch (_) {
      // Ignore close errors in cleanup.
    }
    await writeResult(result);
  }

  if (!result.connected) {
    console.log(`FAIL: CDP connection failed (${result.error || 'unknown error'})`);
    process.exit(1);
  }
  if (!result.twitter_visible) {
    console.log(`FAIL: Twitter not visible (${result.error || 'unknown error'})`);
    process.exit(1);
  }
  console.log('PASS: CDP connection live and Twitter visible');
}

main().catch(async (error) => {
  const result = {
    connected: false,
    twitter_visible: false,
    cdp_url: cdpUrl,
    error: error instanceof Error ? error.message : String(error)
  };
  await writeResult(result);
  console.log(`FAIL: ${result.error}`);
  process.exit(1);
});
