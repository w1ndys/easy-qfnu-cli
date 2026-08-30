package main

import (
	"os"

	"github.com/w1ndys/easy-qfnu-cli/internal/qfnu"
)

func main() {
	os.Exit(qfnu.Run(os.Args[1:], os.Stdout, os.Stderr))
}
