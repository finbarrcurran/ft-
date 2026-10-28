package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestParseInterleavedFlagsAfterQuery(t *testing.T) {
	for _, args := range [][]string{
		{"halving", "--source", "cowen"},
		{"--source", "cowen", "halving"},
		{"halving", "--source=cowen", "--limit", "3"},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		src := fs.String("source", "", "")
		fs.Int("limit", 10, "")
		words := parseInterleaved(fs, args)
		if *src != "cowen" || !reflect.DeepEqual(words, []string{"halving"}) {
			t.Fatalf("%v: source=%q words=%v", args, *src, words)
		}
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("source", "", "")
	if got := parseInterleaved(fs, []string{"risk", "indicator", "--source", "jordi"}); !reflect.DeepEqual(got, []string{"risk", "indicator"}) {
		t.Fatalf("multi-word query: %v", got)
	}
}
