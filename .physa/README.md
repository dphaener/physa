# The map

This is the product's map: why the product is the way it is.

- Read `product/` first. Its rules apply to every change.
- Each folder in `capabilities/` is one promise the product makes. Read the ones related to your task.
- `glossary.md` holds the words to use, and the words to avoid.

Each capability folder holds:

- `index.md`: the promise, a short overview, and links to the files below.
- `rules.md`: every rule. Always read it in full.
- `decisions.md` and `questions.md`: a contents list of every entry, then the entries. Read the contents, then only the entries your task touches.

To cut one entry out by its slug (the first word of the line after its heading), run this in the file:

```
awk '/^### /{h=$0;p=0;next} h&&/^`the-slug`/{print h;p=1} p' decisions.md
```

If the CLI is available, `physa map slice <capability>` does the same for you.

Anchor each commit to the capability it serves with a trailer: `Capability: <folder name>`.
