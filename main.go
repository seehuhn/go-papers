// seehuhn.de/go/paper - tools for managing a store of scientific papers
// Copyright (C) 2026  Jochen Voss <voss@seehuhn.de>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"fmt"
	"os"
)

type command struct {
	name string
	desc string
	help string // full help text, shown by "paper help <name>" and -h
	run  func(args []string) error
}

var commands []command

// helpTopic is a "paper help <name>" topic that is not a command, such
// as the description of the store layout and the paper.json format.
type helpTopic struct {
	name string
	desc string
	help string
}

var helpTopics []helpTopic

// helpFor returns the full help text for a command, for use as the
// flag package's Usage output. It falls back to a bare usage line for
// a command with no help text.
func helpFor(name string) string {
	for _, c := range commands {
		if c.name == name {
			if c.help != "" {
				return c.help
			}
			break
		}
	}
	return "usage: paper " + name + " [arguments]\n"
}

func dispatch(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
		if args != nil && args[0] == "help" && len(args) > 1 {
			return printTopicHelp(args[1])
		}
		fmt.Println("usage: paper <command> [arguments]")
		for _, c := range commands {
			fmt.Printf("  %-8s %s\n", c.name, c.desc)
		}
		fmt.Println("\nadditional help topics:")
		for _, t := range helpTopics {
			fmt.Printf("  %-8s %s\n", t.name, t.desc)
		}
		fmt.Println("\nrun 'paper help <command or topic>' for details")
		return nil
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(args[1:])
		}
	}
	return fmt.Errorf("unknown command %q; run 'paper help' for the command list", args[0])
}

// printTopicHelp prints the full help text for one command or help
// topic to stdout.
func printTopicHelp(name string) error {
	for _, c := range commands {
		if c.name == name {
			fmt.Print(helpFor(name))
			return nil
		}
	}
	for _, t := range helpTopics {
		if t.name == name {
			fmt.Print(t.help)
			return nil
		}
	}
	return fmt.Errorf("unknown help topic %q; run 'paper help' for the list", name)
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "paper: %v\n", err)
		os.Exit(1)
	}
}
