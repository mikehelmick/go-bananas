package test_test

import (
	"testing"

	"github.com/mikehelmick/go-bananas/pkg/test"
)

func TestAdd(t *testing.T) {

	c := test.Add(1, 2)
	if c != 3 {
		t.Fatalf("got the wrong answer...")
	}
}
