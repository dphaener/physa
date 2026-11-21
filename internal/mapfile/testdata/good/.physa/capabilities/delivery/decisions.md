# Delivery · Decisions

## Contents

- [Six delivery attempts](#six-delivery-attempts)

## Entries

### Six delivery attempts
`retry-limit` · decided by dana, accepting a proposal · 2026-03-02 · #1102 #1140 · from docs/vision.md
Six attempts, then dead-letter.
Because: support needs a bounded window.
Rejected:
- Unlimited retries: floods endpoints.
- Three attempts: too short.
