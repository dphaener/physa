// Package other writes the engine's tables, which only internal/state may.
package other

// Add inserts into one table and deletes from another.
func Add() []string {
	return []string{
		"INSERT INTO ideas (slug) VALUES (?)",
		"delete from  events where id = ?",
	}
}
