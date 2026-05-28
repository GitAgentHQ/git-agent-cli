package main

import (
	"fmt"
	"strings"
)

func main() {
	content := "l1\nl2\nl3"
	dLines := strings.Count(content, "\n")
	maxLines := 2

	if dLines > maxLines {
		lines := strings.SplitN(content, "\n", maxLines+1)
		content = strings.Join(lines[:maxLines], "\n")
		fmt.Printf("Truncated: %q\n", content)
	} else {
		fmt.Printf("Not truncated: %q\n", content)
	}
}
