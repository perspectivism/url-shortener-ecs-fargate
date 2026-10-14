// k6 baseline for the redirect path (SPEC §16, M6 gate).
//
// setup() creates LINKS short links through the API, the "redirects" scenario
// resolves them at a constant arrival rate (plus a share of unknown codes that
// exercise the negative cache), a light "stats" scenario reads analytics, and
// teardown() deletes the links again.
//
// Configuration (environment only — the API key is never written to a file):
//   BASE_URL       public base URL (CloudFront; local: http://localhost:8080), required
//   API_URL        API base URL (default BASE_URL; local: http://localhost:8081)
//   LOAD_API_KEY   API key with links:read and links:write, required
//   RATE           redirects per second (default 15: one client IP stays below the
//                  dev WAF redirect rate rule of 1200/min; raise that rule for larger runs)
//   DURATION       steady-state duration (default 2m)
//   LINKS          links created in setup (default 50; the API allows 60 creates/min per key)
//   UNKNOWN_RATIO  share of requests for unknown codes (default 0.05)
//   P99_MS         client-side p99 threshold for redirects in ms (default 300)
//
// Client-side latency includes the network to CloudFront; the SLO (SPEC N1)
// is measured at the ALB (TargetResponseTime) — see scripts/load-report.sh.

import http from "k6/http";
import { check, fail, sleep } from "k6";
import { Counter, Rate } from "k6/metrics";

const BASE_URL = (__ENV.BASE_URL || "").replace(/\/$/, "");
const API_URL = (__ENV.API_URL || BASE_URL).replace(/\/$/, "");
const API_KEY = __ENV.LOAD_API_KEY || "";
const RATE = parseInt(__ENV.RATE || "15", 10);
const DURATION = __ENV.DURATION || "2m";
const LINKS = parseInt(__ENV.LINKS || "50", 10);
const UNKNOWN_RATIO = parseFloat(__ENV.UNKNOWN_RATIO || "0.05");
const P99_MS = parseInt(__ENV.P99_MS || "300", 10);

const UA = "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0 shortener-load";

const unexpected = new Rate("unexpected_status");
const redirects = new Counter("redirects_ok");

export const options = {
  discardResponseBodies: true,
  scenarios: {
    redirects: {
      executor: "constant-arrival-rate",
      exec: "redirect",
      rate: RATE,
      timeUnit: "1s",
      duration: DURATION,
      preAllocatedVUs: Math.max(10, RATE),
      maxVUs: Math.max(50, RATE * 4),
    },
    stats: {
      executor: "constant-arrival-rate",
      exec: "stats",
      rate: 1,
      timeUnit: "1s",
      duration: DURATION,
      preAllocatedVUs: 2,
      maxVUs: 5,
    },
  },
  thresholds: {
    unexpected_status: ["rate<0.01"],
    "http_req_duration{scenario:redirects}": [`p(99)<${P99_MS}`],
    "http_req_failed{scenario:redirects}": ["rate<0.01"],
  },
  summaryTrendStats: ["avg", "min", "med", "p(90)", "p(95)", "p(99)", "max"],
};

function headers(extra) {
  return Object.assign({ "User-Agent": UA, Authorization: `Bearer ${API_KEY}` }, extra || {});
}

// API writes are limited per key (60/min): setup and teardown honour 429 +
// Retry-After instead of failing or leaving links behind.
function withRetry(send) {
  for (let attempt = 0; ; attempt++) {
    const res = send();
    if (res.status !== 429 || attempt >= 5) {
      return res;
    }
    sleep(parseInt(res.headers["Retry-After"] || "5", 10));
  }
}

export function setup() {
  if (!BASE_URL || !API_KEY) {
    fail("BASE_URL and LOAD_API_KEY are required");
  }
  const run = Date.now();
  const codes = [];
  for (let i = 0; i < LINKS; i++) {
    const res = withRetry(() =>
      http.post(
        `${API_URL}/api/v1/links`,
        JSON.stringify({ url: `https://example.com/load/${run}/${i}` }),
        {
          headers: headers({ "Content-Type": "application/json" }),
          responseType: "text",
          responseCallback: http.expectedStatuses(201, 429),
          tags: { name: "setup create" },
        },
      ),
    );
    if (res.status !== 201) {
      fail(`create link: status ${res.status}`);
    }
    codes.push(res.json("code"));
  }
  return { codes, run };
}

export function redirect(data) {
  const unknown = Math.random() < UNKNOWN_RATIO;
  // A small set of unknown codes repeats, so the negative cache is exercised.
  const code = unknown ? `zzLoad${__ITER % 10}` : data.codes[Math.floor(Math.random() * data.codes.length)];
  const want = unknown ? 404 : 302;
  const res = http.get(`${BASE_URL}/${code}`, {
    redirects: 0,
    headers: { "User-Agent": UA },
    responseCallback: http.expectedStatuses(want),
    tags: { name: unknown ? "GET /{unknown}" : "GET /{code}" },
  });
  const ok = check(res, { [`status ${want}`]: (r) => r.status === want });
  unexpected.add(!ok);
  if (ok && !unknown) {
    redirects.add(1);
  }
}

export function stats(data) {
  const code = data.codes[Math.floor(Math.random() * data.codes.length)];
  const res = http.get(`${API_URL}/api/v1/links/${code}/stats`, {
    headers: headers(),
    tags: { name: "GET /api/v1/links/{code}/stats" },
  });
  unexpected.add(!check(res, { "stats 200": (r) => r.status === 200 }));
}

export function teardown(data) {
  let deleted = 0;
  for (const code of data.codes) {
    const res = withRetry(() =>
      http.del(`${API_URL}/api/v1/links/${code}`, null, {
        headers: headers(),
        responseCallback: http.expectedStatuses(204, 429),
        tags: { name: "teardown delete" },
      }),
    );
    deleted += res.status === 204 ? 1 : 0;
  }
  if (deleted !== data.codes.length) {
    fail(`teardown deleted ${deleted}/${data.codes.length} links`);
  }
}
