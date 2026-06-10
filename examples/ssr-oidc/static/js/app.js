// Demonstrates reading the CSRF token from the meta tag the framework renders,
// so fetch()-based requests can include it via the X-CSRF-Token header.
(function () {
  "use strict";
  const meta = document.querySelector('meta[name="csrf-token"]');
  if (meta) {
    window.csrfToken = meta.getAttribute("content");
  }
})();
