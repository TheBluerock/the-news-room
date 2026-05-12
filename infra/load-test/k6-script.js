/**
 * k6 load test for the AI Newsroom platform.
 *
 * Scenarios:
 *   1. article_trigger  — POST /api/agent/trigger (10 req/s × 60s per market)
 *   2. moderation_queue — GET  /api/moderation/queue (admin role, read-heavy)
 *   3. admin_approve    — POST /api/moderation/approve (low rate, admin action)
 *
 * Thresholds: p99 < 2s, error rate < 1%, moderation queue p95 < 500ms.
 *
 * Run: k6 run infra/load-test/k6-script.js
 * Override base URL: k6 run -e BASE_URL=https://staging.newsroom.example infra/load-test/k6-script.js
 */

import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

const BASE_URL = __ENV.BASE_URL || "http://localhost:80";
const ADMIN_TOKEN = __ENV.ADMIN_TOKEN || "dev-admin-token";

const errorRate = new Rate("errors");
const triggerLatency = new Trend("trigger_latency_ms");
const queueLatency = new Trend("queue_latency_ms");
const approveLatency = new Trend("approve_latency_ms");

const MARKETS = ["italy", "usa", "china"];

export const options = {
  scenarios: {
    article_trigger: {
      executor: "constant-arrival-rate",
      rate: 30,          // 10 req/s × 3 markets
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 40,
      maxVUs: 80,
      exec: "triggerArticle",
    },
    moderation_queue: {
      executor: "constant-arrival-rate",
      rate: 20,
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 10,
      maxVUs: 20,
      exec: "readQueue",
    },
    admin_approve: {
      executor: "constant-arrival-rate",
      rate: 2,
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 5,
      maxVUs: 10,
      exec: "approveArticle",
    },
  },
  thresholds: {
    // Global error budget
    errors: [{ threshold: "rate<0.01", abortOnFail: true }],
    // Latency SLAs
    trigger_latency_ms:  ["p(99)<2000"],
    queue_latency_ms:    ["p(95)<500"],
    approve_latency_ms:  ["p(99)<2000"],
    // http_req_duration covers all scenarios as a backstop
    http_req_duration:   ["p(99)<3000"],
    http_req_failed:     ["rate<0.01"],
  },
};

const adminHeaders = {
  "Content-Type": "application/json",
  "Authorization": `Bearer ${ADMIN_TOKEN}`,
  "X-User-Role": "admin",
};

export function triggerArticle() {
  const market = MARKETS[Math.floor(Math.random() * MARKETS.length)];
  const payload = JSON.stringify({
    market,
    topic: `load-test-topic-${__ITER}`,
    requested_by: "k6-load-test",
  });

  const start = Date.now();
  const res = http.post(`${BASE_URL}/api/agent/trigger`, payload, {
    headers: { "Content-Type": "application/json" },
    tags: { name: "article_trigger", market },
  });
  triggerLatency.add(Date.now() - start);

  const ok = check(res, {
    "trigger 2xx": (r) => r.status >= 200 && r.status < 300,
    "trigger has event_id": (r) => {
      try { return !!JSON.parse(r.body).event_id; } catch { return false; }
    },
  });
  errorRate.add(!ok);
  sleep(0.1);
}

export function readQueue() {
  const market = MARKETS[Math.floor(Math.random() * MARKETS.length)];

  const start = Date.now();
  const res = http.get(`${BASE_URL}/api/moderation/queue?market=${market}&limit=20`, {
    headers: adminHeaders,
    tags: { name: "moderation_queue", market },
  });
  queueLatency.add(Date.now() - start);

  const ok = check(res, {
    "queue 200": (r) => r.status === 200,
    "queue is array": (r) => {
      try { return Array.isArray(JSON.parse(r.body)); } catch { return false; }
    },
  });
  errorRate.add(!ok);
  sleep(0.05);
}

export function approveArticle() {
  // Use a deterministic fake article_id per VU — moderation service returns 404
  // for unknown IDs which is expected in load test; we assert the service responds
  // without 5xx (no crash under concurrent approve traffic).
  const fakeID = `00000000-0000-0000-0000-${String(__VU).padStart(12, "0")}`;
  const payload = JSON.stringify({ article_id: fakeID, notes: "k6-load-test" });

  const start = Date.now();
  const res = http.post(`${BASE_URL}/api/moderation/approve`, payload, {
    headers: adminHeaders,
    tags: { name: "admin_approve" },
  });
  approveLatency.add(Date.now() - start);

  // 404 is acceptable — fake IDs won't be in the queue
  const ok = check(res, {
    "approve no 5xx": (r) => r.status < 500,
  });
  errorRate.add(!ok);
  sleep(0.5);
}
