// k6 crash test - pushes until server fails
// Usage: k6 run --vus 1000 --duration 5m k6_crash_test.js

import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

const errors = new Rate('errors');
const responseTime = new Trend('response_time');
const successCount = new Counter('success_count');
const failureCount = new Counter('failure_count');

export const options = {
  stages: [
    { duration: '30s', target: 100 },   // Start moderate
    { duration: '1m', target: 500 },    // High load
    { duration: '2m', target: 1000 },   // Very high load
    { duration: '2m', target: 2000 },  // Crash load
    { duration: '30s', target: 0 },     // Stop
  ],
  noConnectionReuse: true,
  noVUConnectionReuse: true,
  batch: 200,
  batchPerHost: 200,
};

export function setup() {
  const res = http.post(
    `${BASE_URL}/auth/account`,
    '{}',
    { headers: { 'Content-Type': 'application/json' } }
  );
  
  if (res.status !== 201) {
    throw new Error(`Setup failed: ${res.status}`);
  }
  
  const data = JSON.parse(res.body);
  
  // Pre-populate some keys
  const headers = {
    'X-API-Key': data.apiKey,
    'Content-Type': 'application/json',
  };
  
  for (let i = 0; i < 100; i++) {
    http.post(
      `${BASE_URL}/v1/${data.tenantID}/SET`,
      JSON.stringify({
        key: `prewarm_${i}`,
        value: `value_${i}`,
      }),
      { headers }
    );
  }
  
  return data;
}

export default function (data) {
  const headers = {
    'X-API-Key': data.apiKey,
    'Content-Type': 'application/json',
  };
  
  const key = `key_${__VU}_${__ITER}`;
  const payload = JSON.stringify({ key: key, value: 'test_value' });
  
  const start = Date.now();
  const res = http.post(
    `${BASE_URL}/v1/${data.tenantID}/SET`,
    payload,
    { headers }
  );
  const duration = Date.now() - start;
  
  responseTime.add(duration);
  
  const success = res.status === 200;
  if (success) {
    successCount.add(1);
  } else {
    failureCount.add(1);
    console.log(`Error: ${res.status} - ${res.body}`);
  }
  
  errors.add(!success);
  
  check(res, {
    'status is 200': (r) => r.status === 200,
    'response time < 1000ms': () => duration < 1000,
  });
}

export function handleSummary(data) {
  return {
    'summary.json': JSON.stringify({
      maxVUs: data.metrics.vus ? data.metrics.vus.max : 0,
      requests: data.metrics.http_reqs ? data.metrics.http_reqs.count : 0,
      errorRate: data.metrics.errors ? data.metrics.errors.rate : 0,
      avgResponseTime: data.metrics.response_time ? data.metrics.response_time.avg : 0,
      p95ResponseTime: data.metrics.response_time ? data.metrics.response_time['p(95)'] : 0,
      maxResponseTime: data.metrics.response_time ? data.metrics.response_time.max : 0,
      failedRequests: data.metrics.failure_count ? data.metrics.failure_count.count : 0,
      successfulRequests: data.metrics.success_count ? data.metrics.success_count.count : 0,
    }),
  };
}
