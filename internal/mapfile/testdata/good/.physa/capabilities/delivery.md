---
promise: Webhooks reach customers at least once.
stewards: [dana, sam]
---

# Delivery

## Rules

### Retries go through the queue
`queue-retries` · #1290 a1b2c3d
Retry only by enqueueing a job.
Because: a deploy dropped retries.

## Decisions

### Six delivery attempts
`retry-limit` · decided by dana, accepting a proposal · 2026-03-02 · #1102 #1140 · from docs/vision.md
Six attempts, then dead-letter.
Because: support needs a bounded window.
Rejected:
- Unlimited retries: floods endpoints.
- Three attempts: too short.
