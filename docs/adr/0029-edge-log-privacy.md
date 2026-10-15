# ADR-0029: Edge log privacy — field allowlist, WAF log filter, no request sampling

- **Status:** Accepted (extends [ADR-0018](0018-privacy-hmac-visitor-ids-referrer-host-only.md) to the edge; changes the WAF logging of [ADR-0012](0012-cloudfront-vpc-origin-internal-alb-waf.md))
- **Date:** 2026-10-15
- **Deciders:** project owner, coding agent

## Context

[ADR-0018](0018-privacy-hmac-visitor-ids-referrer-host-only.md) keeps raw IPs
out of the application's data, and the docs said "no raw IPs stored anywhere"
(SPEC N6). An external review found that the edge contradicted this, and
worse:

- **CloudFront standard logs** recorded every field, including the viewer IP
  (`c-ip`, `x-forwarded-for`), port, full referrer, user agent, query string
  and cookies.
- **WAF logs** always contain the client IP. Field redaction cannot remove it.
- **WAF request sampling** was on. Sampled requests are kept for 3 hours
  and shown in the console and API, and AWS states that log redaction
  "has no impact on request sampling". The samples therefore showed the
  `Authorization` header, which is an API key.

## Decision Drivers

- No API key may be readable outside the API (SPEC §10)
- Store as little personal data as operations allow (SPEC N6)
- Keep enough data to investigate abuse and tune WAF rules

## Considered Options

1. Keep full logs and narrow the privacy claim
2. Turn off CloudFront and WAF logging
3. **Log only the fields and requests operations need; turn off sampling**

## Decision

Chosen option: **3**.

- **WAF request sampling is off** on the web ACL and every rule. WAF logs,
  where `Authorization` is redacted, replace the samples. Data protection
  configuration was the alternative, but it adds configuration that buys
  nothing over the logs.
- **CloudFront logs use a field allowlist** (`record_fields`): time, edge,
  request ID, host, method, path, protocol, status, sizes, timings, cache
  result and TLS details. Excluded: `c-ip`, `c-port`, `x-forwarded-for`,
  `cs(Referer)`, `cs(User-Agent)`, `cs-uri-query` and `cs(Cookie)`.
- **WAF logs keep only blocked and counted requests** (`logging_filter`, default
  `DROP`). Those records include the client IP, which AWS WAF cannot redact,
  and are what abuse investigation and rule tuning need. They are kept 14 days
  in dev and 90 in prod, encrypted with the environment CMK.
- Terraform tests fail if sampling is enabled, if a dropped CloudFront field
  returns, or if the WAF filter keeps allowed requests.

**Deviation recorded:** SPEC N6 says no raw IPs are stored anywhere. WAF logs
of blocked or counted requests keep the client IP for the retention period
above. The privacy claim in the docs now says exactly that.

## Consequences

### Positive

- API keys never appear in WAF samples, logs or CloudFront logs.
- Ordinary visitors leave no IP address in any log.
- Logging costs fall: allowed requests are no longer written to WAF logs.

### Negative

- Diagnosing a single allowed request at the edge needs the request ID and
  the application's access log; there is no IP to search by.
- The WAF console shows no sampled requests; use Logs Insights on the WAF log group.

## Interview Angle

> "Privacy has to hold at every hop, not only in the database. The edge logs
> an allowlist of fields, WAF keeps only blocked and counted requests (it
> can't redact IPs), and request sampling is off because it ignores log redaction
> and would show API keys."
