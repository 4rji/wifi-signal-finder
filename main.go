package main

import (
	"os"

	"wifi-radar/internal/serverapp"
)

func main() {
	serverapp.Run(os.Args[1:])
}
