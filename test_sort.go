package main

import (
	"fmt"
	"sort"
)

type bucket struct {
	dir   string
	files []string
}

func main() {
	maxCommitGroups := 5
	buckets := []bucket{
		{"a", []string{"1"}},
		{"b", []string{"2", "3"}},
		{"c", []string{"4", "5", "6"}},
		{"d", []string{"7", "8", "9", "10"}},
		{"e", []string{"11", "12", "13", "14", "15"}}, // 5th
		{"f", []string{"16", "17", "18"}},
		{"g", []string{"19"}},
	}

	sort.SliceStable(buckets[maxCommitGroups-1:], func(i, j int) bool {
		a := buckets[maxCommitGroups-1+i]
		c := buckets[maxCommitGroups-1+j]
		return len(a.files) < len(c.files)
	})

	mergeTarget := &buckets[maxCommitGroups-1]
	surplus := buckets[maxCommitGroups:]
	for _, s := range surplus {
		mergeTarget.files = append(mergeTarget.files, s.files...)
	}
	buckets = buckets[:maxCommitGroups]

	for _, b := range buckets {
		fmt.Printf("%s: %d files\n", b.dir, len(b.files))
	}
}
