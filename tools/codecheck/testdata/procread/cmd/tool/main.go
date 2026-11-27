// Command tool reads the environment, as a command may.
package main

import "os"

func main() {
	_ = os.Getenv("HOME")
}
