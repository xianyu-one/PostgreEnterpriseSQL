package main

import (
	"os"

	"pg_mgr/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
