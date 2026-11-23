// Package mapping proposes a product's promise and capabilities from the
// gathered sources, read whole, in one prompt-only session under an
// appetite, and checks the reply against the gather. Every capability it
// proposes is a promise the product makes to its users, with the PRs or
// changes that best show it and the docs that hold it, and what the sources leave uncertain comes back
// as a question, never a guess. It writes nothing to the repo: the proposal
// goes to the store for init's next phase.
package mapping

// Product is the product's own promise, as the product's folder will hold it.
type Product struct {
	// Promise is one sentence on one line; empty when the session's was
	// dropped.
	Promise string `json:"promise"`
}

// Question is an undecided matter the sources suggest but do not settle.
type Question struct {
	// Question is the matter, one line ending with a question mark.
	Question string `json:"question"`
	// SuggestedBy names the PRs ("#12") and changes (short ids) that
	// suggested it.
	SuggestedBy []string `json:"suggested_by"`
}

// Capability is one proposed capability.
type Capability struct {
	// Name is the capability's name, a slug as mapfile.ValidSlug reads one.
	Name string `json:"name"`
	// Promise is what the product promises its users, on one line.
	Promise string `json:"promise"`
	// ShownBy names the PRs ("#12") and changes (short ids) that best show
	// the promise.
	ShownBy []string `json:"shown_by"`
	// Docs are the paths of the gathered docs that hold its entries.
	Docs []string `json:"docs"`
	// Questions are the questions about this capability.
	Questions []Question `json:"questions"`
	// Unshown says nothing gathered shows the capability: the session named
	// no evidence, or none of what it named was gathered. Check sets it.
	Unshown bool `json:"unshown"`
}

// Reply is what the session wrote, in shape only: Parse has decoded it and
// Check has not yet held it against the gather.
type Reply struct {
	Product      Product
	Capabilities []Capability
	Questions    []Question
}

// Proposal is the checked proposal: the product's promise, the capabilities
// and the product-wide questions, every name a slug and every PR or change
// one that was gathered.
type Proposal struct {
	Product      Product      `json:"product"`
	Capabilities []Capability `json:"capabilities"`
	Questions    []Question   `json:"questions"`
}

// Malformed is one item of the session's reply that is not a capability or a
// question, kept so that it is reported and never dropped silently.
type Malformed struct {
	// Raw is the item as the session wrote it.
	Raw string `json:"raw"`
	// Reason says what is wrong with it.
	Reason string `json:"reason"`
}

// The values of Drop.What.
const (
	WhatPromise    = "product promise"
	WhatCapability = "capability"
	WhatRef        = "ref"
	WhatQuestion   = "question"
)

// Drop is one thing Check took out of the reply, with its reason.
type Drop struct {
	// What is one of the What constants.
	What string `json:"what"`
	// Capability is the capability the thing belonged to; empty for the
	// product's promise, a product-wide question, or a capability itself.
	Capability string `json:"capability"`
	// Value is the thing as the session wrote it: a name, a ref, a
	// question, a promise.
	Value string `json:"value"`
	// Reason says why it was dropped.
	Reason string `json:"reason"`
}

// Dropped lists everything Check took out, in the reply's order.
type Dropped []Drop
