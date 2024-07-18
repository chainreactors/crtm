package utils

import (
	"fmt"
	"testing"
)

func TestFetchToolList(t *testing.T) {
	tools, err := FetchToolList()
	if err != nil {
		t.Errorf(err.Error())
		return
	}
	for _, t := range tools {
		fmt.Println(t)
	}

}
