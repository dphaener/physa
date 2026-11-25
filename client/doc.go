// Package client is the public way into the engine's local API, for views, harnesses and other programs outside Physa's module: it dials the engine's socket under the physa.v1 subprotocol, calls methods by name, receives the messages the engine sends of its own, and holds the input and output types of the methods for ideas, pursuits and evidence.
//
// It imports nothing of the engine's store, forge or version control, so a
// program built on it stays small. The engine's API is typed with these same
// types, so a change to one is a change to the other.
package client
