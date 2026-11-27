## Splitting an area that is too large

Decide first, from the file list and a quick look, whether you can walk this area well in one session. If it is too large (you could not read enough of its files, docs and tests to judge its capabilities, or it holds several subsystems that each keep many promises), split it instead of naming capabilities: answer with one fenced `json` block, with nothing after it, holding one object with the key `split`, a list of two or more parts, each an object with exactly the keys `name` (kebab-case, naming a family of promises, never a layer), `promise` (one sentence on one line) and `sources` (paths as above). There is no maximum and no number to aim for: split along the area's own seams.

Every source file of the area falls in exactly one part. Split by promise, never by layer: a part keeps its promises' server and client files together. Each part is then walked on its own, as you would have walked the area, and a part is never split again. Decide early, before reading deeply, since what you read for a split is not carried into the sessions that follow.

```json
{"split": [
  {"name": "order-refunds", "promise": "People refund an order in full or in part, and the money returns to how they paid.", "sources": ["server/refunds/", "client/refund_form.ts"]},
  {"name": "gift-cards", "promise": "People buy, send and spend gift cards, and see what is left on them.", "sources": ["server/gift_cards/"]}
]}
```

When the area is one walk, name its capabilities as above.
