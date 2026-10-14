# ADR-0005: Separate links and analytics tables

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

The links table is small, read-heavy and on the critical path of every
redirect. Click analytics are write-heavy (one transactional write per click,
plus dimension counters) and never on the redirect path (SPEC §5).

## Decision Drivers

- Blast radius: an analytics write storm must not throttle redirects
- Independent throughput caps, alarms and cost attribution
- Different item lifecycles (dedupe markers expire after 3 days)
- Least privilege: the redirect task never needs analytics access

## Considered Options

1. Single table design (links and stats in one table)
2. Two tables: `links` and `analytics`
3. Analytics only in S3 + Athena (no counters in DynamoDB)

## Decision

Chosen option: **2 — two tables**, `shortener-{env}-links` and
`shortener-{env}-analytics`, each with its own on-demand max-throughput cap and
throttle alarm.

## Consequences

### Positive

- A spike in clicks can only throttle the worker (which retries via SQS), never redirects.
- IAM is scoped per table: redirect reads `links` only; the worker writes `analytics` only.
- Capacity, alarms and cost are visible per workload.

### Negative

- Two tables to provision, back up and monitor.
- No cross-table transactions — none are needed.

## Interview Angle

> "Reads that serve users and writes that count clicks have different scaling
> and failure modes, so they live in different tables: a click storm can
> throttle analytics, but it can never take down redirects."
