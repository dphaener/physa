// Package state writes the engine's tables, which it may.
package state

// Add inserts into one table and deletes from another.
func Add() []string {
	return []string{
		"INSERT INTO ideas (slug) VALUES (?)",
		"delete from  events where id = ?",
	}
}
