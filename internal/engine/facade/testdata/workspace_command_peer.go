package main

import (
	"fmt"
	"io"
	"os"
)

// The peer reads the selected workspace, a relative child and exact stdin bytes.
func main() {
	if err := os.Chdir(os.Getenv("FORMAL_GATES_WORKSPACE")); err != nil {
		panic(err)
	}
	for _, path := range []string{"identity", "relative directory/identity"} {
		body, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		if _, err := os.Stdout.Write(body); err != nil {
			panic(err)
		}
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		panic(err)
	}
	if len(os.Args) == 3 && os.Args[1] == "argument identity" {
		fmt.Printf("<%s>", os.Args[2])
	} else if len(os.Args) != 1 {
		panic("unexpected workspace peer arguments")
	}
}
