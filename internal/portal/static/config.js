// The deployment configuration of the portal front-end.
//
// AGHub serves this page itself, so the defaults below are what the bundled
// copy needs.  The "portal deployment" dialog in the admin UI generates a
// filled-in copy of this file for a front-end that is deployed elsewhere.
window.AGHUB_PORTAL_CONFIG = {
  // The address of the portal API as the browser reaches it, for example
  // "https://dns.example.com:3004".  Empty means the origin the page was
  // served from.
  apiBase: "",
};
