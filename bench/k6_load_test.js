// k6 load test for Redixis API
// Usage: k6 run --vus 100 --duration 30s k6_load_test.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// Custom metrics
const apiCallErrors = new Rate('api_call_errors');
const responseTime = new Trend('response_time');
const requestsPerSecond = new Counter('requests_per_second');

export const options = {
  stages: [
    { duration: '10s', target: 10 },   // Ramp up to 10 users
    { duration: '20s', target: 50 },   // Ramp up to 50 users
    { duration: '30s', target: 100 },  // Ramp up to 100 users
    { duration: '30s', target: 200 },  // Ramp up to 200 users
    { duration: '30s', target: 500 },  // Ramp up to 500 users
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],   // 95% of requests under 500ms
    http_req_failed: ['rate<0.1'],      // Error rate under 10%
  },
};

// Shared state across VUs (virtual users)
let tenantID = null;
let apiKey = null;

export function setup() {
  // Create account once and share credentials
  const res = http.post(
    `${BASE_URL}/auth/account`,
    '{}',
    { headers: { 'Content-Type': 'application/json' } }
  );
  
  const success = check(res, {
    'account created': (r) => r.status === 201,
  });
  
  if (!success) {
    throw new Error(`Failed to create account: ${res.status}`);
  }
  
  const data = JSON.parse(res.body);
  return {
    tenantID: data.tenant_id,
    apiKey: data.api_key,
  };
}

export default function (data) {
  const headers = {
    'X-API-Key': data.apiKey,
    'Content-Type': 'application/json',
  };
  
  const key = `load_test_key_${__VU}_${__ITER}`;
  
  // Test SET
  {
    const payload = JSON.stringify({
      key: key,
      value: `value_${__ITER}`,
      ttl_seconds: 300,
    });
    
    const start = Date.now();
    const res = http.post(
      `${BASE_URL}/v1/${data.tenantID}/SET`,
      payload,
      { headers }
    );
    const duration = Date.now() - start;
    
    responseTime.add(duration);
    requestsPerSecond.add(1);
    
    const success = check(res, {
      'SET status is 200': (r) => r.status === 200,
    });
    
    apiCallErrors.add(!success);
  }
  
  // Test GET
  {
    const payload = JSON.stringify({ key: key });
    
    const start = Date.now();
    const res = http.post(
      `${BASE_URL}/v1/${data.tenantID}/GET`,
      payload,
      { headers }
    );
    const duration = Date.now() - start;
    
    responseTime.add(duration);
    requestsPerSecond.add(1);
    
    const success = check(res, {
      'GET status is 200 or 404': (r) => r.status === 200 || r.status === 404,
    });
    
    apiCallErrors.add(!success);
  }
  
  // Test INCR (every 10th iteration to avoid too many increments on same key)
  if (__ITER % 10 === 0) {
    const counterKey = `counter_${data.tenantID}`;
    
    // First set the counter if it doesn't exist
    http.post(
      `${BASE_URL}/v1/${data.tenantID}/SET`,
      JSON.stringify({ key: counterKey, value: '0' }),
      { headers }
    );
    
    const payload = JSON.stringify({ key: counterKey });
    
    const start = Date.now();
    const res = http.post(
      `${BASE_URL}/v1/${data.tenantID}/INCR`,
      payload,
      { headers }
    );
    const duration = Date.now() - start;
    
    responseTime.add(duration);
    requestsPerSecond.add(1);
    
    const success = check(res, {
      'INCR status is 200': (r) => r.status === 200,
    });
    
    apiCallErrors.add(!success);
  }
  
  // Small sleep to prevent hammering
  sleep(0.01); // 10ms
}

export function teardown(data) {
  // Cleanup - could delete test keys here if needed
  console.log(`Test completed. Tenant ID: ${data.tenantID}`);
}
