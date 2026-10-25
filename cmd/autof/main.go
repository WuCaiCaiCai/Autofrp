package main

import (
	"fmt"
	"os"

	"github.com/WuCaiCaiCai/Autofrp/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m[x]\033[0m %v\n", err)
		os.Exit(1)
	}
}
