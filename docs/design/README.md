# Design references

The design system itself is `internal/design`: tokens, component styles, fonts and the brand book. This folder holds what explains it.

- `components.html`: every component, live from `internal/design`. Open it in a browser from a checkout.
- The prototypes, frozen as they were when each view was settled (docs/architecture/06). Each carries its own copy of the tokens, so later changes to `internal/design` do not reach them.
  - `network-studies.html`: the network view's studies; A4 Grown is the one chosen.
  - `needs-you.html`: Needs you and Running now.
  - `capability-page.html`: a capability's page, on Slices and Rollback.
  - `idea-page.html`: an idea's page, on a running bet and a withering idea.
  - `watch-session.html`: watching a live session, running, waiting and stalled.

The network drawing as the server renders it is on the branch `spike/network-view`, until the step 9 bet builds it into the core.
