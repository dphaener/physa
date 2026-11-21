# The map

This is the product's map: why the product is the way it is.

- Read `product/` first. Its rules apply to every change.
- Each folder in `capabilities/` is one promise the product makes. Read the ones your task touches.
- `glossary.md` holds the words to use, and the words to avoid.

Each capability folder holds:

- `index.md`: the promise, a short overview, and links to the files below.
- `rules.md`: every rule. Always read it in full; there are no more to fetch.
- `decisions.md` and `questions.md`: a contents list of every entry, then the entries. Read the contents, then take only the entries your task touches.

To cut one entry out by its slug (the first word of the line after its heading), run this in the file:

```
awk '/^### /{h=$0;p=0;next} h&&/^`the-slug`/{print h;p=1} p' decisions.md
```

With the CLI, `physa map slice <capability>` gives the rules and the headings of the decisions and questions, and `physa map slice --entry <slug>...` gives the entries you choose.

Which entries to take:

- A decision bears on your task if its heading names something you will change, choose between, or might undo. When in doubt, take it.
- Read each `Rejected:` list. Those options were tried or argued and turned down; don't bring one back without saying why.
- An open question your task would answer: don't settle it silently. Ask, or say in your PR how you answered it.
- Slice another capability only when your plan names it.
- If your task would break a rule or reverse a decision, stop and ask.

Anchor each commit to the capability it serves with a trailer: `Capability: <folder name>`.
