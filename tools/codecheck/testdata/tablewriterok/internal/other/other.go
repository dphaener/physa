// Package other reads the engine's tables and writes none of them.
package other

// Queries holds a read, a write to a look-alike table and prose.
var Queries = []string{
	"SELECT * FROM ideas",
	"INSERT INTO ideas_old (x) VALUES (1)",
	"update plans for the bet",
}
