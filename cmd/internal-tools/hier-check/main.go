package main

import (
	"fmt"
	"os"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: hier-check <dump.xml> [selector ...]")
		os.Exit(2)
	}
	content, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tree, err := hierarchy.Parse(string(content))
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}
	fmt.Printf("parsed %d elements\n", len(tree.Elements))
	for _, selector := range os.Args[2:] {
		elements := tree.FindAll(selector)
		fmt.Printf("%s -> %d matches\n", selector, len(elements))
		for _, element := range elements {
			x, y := element.Bounds.Center()
			fmt.Printf("  id=%q text=%q center=%d,%d\n", element.ResourceID, element.Text, x, y)
		}
	}
}
