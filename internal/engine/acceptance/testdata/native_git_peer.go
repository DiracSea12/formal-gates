// This executable records real Git calls and interrupts one selected native effect.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	arguments := os.Args[1:]
	command := exec.Command(os.Getenv("CANDIDATE_NATIVE_GIT"), arguments...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	result := 0
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			result = exit.ExitCode()
		} else {
			panic(err)
		}
	}
	directory, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	log, err := os.OpenFile(os.Getenv("CANDIDATE_NATIVE_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	if _, err := fmt.Fprintln(log, directory+"\t"+strings.Join(arguments, "\t")); err != nil {
		panic(err)
	}
	if err := log.Close(); err != nil {
		panic(err)
	}
	operation := arguments
	for len(operation) >= 2 && operation[0] == "-c" {
		operation = operation[2:]
	}
	match := false
	if len(operation) >= 2 {
		switch os.Getenv("CANDIDATE_NATIVE_STAGE") + ":" + operation[0] {
		case "workspace:worktree":
			match = operation[1] == "add"
		case "cleanup:worktree":
			match = operation[1] == "remove"
		case "promotion:merge":
			match = operation[1] == "--ff-only"
		case "squash:reset":
			match = operation[1] == "--soft"
		case "track:add":
			match = operation[1] == "--all"
		case "pin:update-ref":
			match = true
		case "artifact:restore":
			match = strings.HasPrefix(operation[1], "--source=")
		case "freeze:commit":
			match = operation[1] == "-m" && len(operation) > 2 && strings.HasPrefix(operation[2], "formal-gates candidate ")
		case "delivery:commit":
			match = operation[1] == "-m" && len(operation) > 2 && strings.HasPrefix(operation[2], "formal-gates delivery ")
		}
	}
	if result == 0 && match {
		marker, err := os.OpenFile(os.Getenv("CANDIDATE_NATIVE_MARKER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			if err := marker.Close(); err != nil {
				panic(err)
			}
			parent, err := os.FindProcess(os.Getppid())
			if err != nil {
				panic(err)
			}
			if err := parent.Kill(); err != nil {
				panic(err)
			}
		} else if !os.IsExist(err) {
			panic(err)
		}
	}
	os.Exit(result)
}
