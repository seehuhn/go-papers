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
	run  func(args []string) error
}

var commands []command

func dispatch(args []string) error {
	if len(args) == 0 || args[0] == "help" {
		fmt.Println("usage: paper <command> [arguments]")
		for _, c := range commands {
			fmt.Printf("  %-8s %s\n", c.name, c.desc)
		}
		return nil
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(args[1:])
		}
	}
	return fmt.Errorf("unknown command %q; run 'paper help' for the command list", args[0])
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "paper: %v\n", err)
		os.Exit(1)
	}
}
