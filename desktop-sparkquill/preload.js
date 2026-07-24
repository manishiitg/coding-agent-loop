const { contextBridge } = require('electron')

// The packaged app serves the frontend from the same origin as the API, so the
// web app should talk to its own origin rather than the hardcoded dev default
// (http://127.0.0.1:8010) — which would be wrong whenever the port shifted
// because 8010 was taken. In the browser this bridge simply isn't there and
// the app falls back to VITE_FAMILY_API, so dev is unaffected.
contextBridge.exposeInMainWorld('sparkquill', {
  isDesktop: true,
  apiBaseUrl: () => window.location.origin,
})
