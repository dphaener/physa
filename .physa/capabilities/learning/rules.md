# Learning · Rules

## Contents

- [Learn at the PR](#learn-at-the-pr)
- [Non-blocking unless the team says otherwise](#non-blocking-unless-the-team-says-otherwise)
- [The map update PR never merges itself](#the-map-update-pr-never-merges-itself)
- [Every learn commit says what it learned through](#every-learn-commit-says-what-it-learned-through)

## Entries

### Learn at the PR
`learn-at-pr` · from docs/architecture/02-init-and-learning.md
The map learns where every agent's work ends up: the PR. While it is open, the change is checked against the map; after it merges, its review is learned from.
Because: it is the one place every agent's work passes through, whichever agent wrote it.

### Non-blocking unless the team says otherwise
`non-blocking` · from docs/architecture/02-init-and-learning.md
The PR check reports a neutral status by default. Teams make specific findings blocking by policy.
Because: a new tool that blocks merges gets removed on day two.

### The map update PR never merges itself
`no-self-merge` · from docs/architecture/02-init-and-learning.md
Everything learned after merge is batched into one map update PR that stewards review.
Because: nothing changes silently.

### Every learn commit says what it learned through
`learned-through-every-commit` · 9730220
Every commit learning makes records the change it has learned through, even on a first run with no forge, and the next run starts from that record rather than from the ids it learned.
Because: the ids alone get lost when the learn PR is squash-merged.
