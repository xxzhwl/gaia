// Package es 包注释
// @author wanlizhan
// @created 2025-12-12
package es

import (
	"testing"

	"github.com/xxzhwl/gaia"
)

func TestSimpleSearch(t *testing.T) {
	client, err := NewClient([]string{"http://43.139.35.39:7901/"}, "elastic", "Elastic_6dXxmb")
	if err != nil {
		t.Fatal(err)
	}

	res, err := client.SimpleSearch(SimpleSearchArg{
		Index: "sys_log",
		Sorts: []SortKv{{
			Name: "LogTimeStamp",
			Desc: true,
		}},
		From:   0,
		Size:   10,
		Must:   nil,
		Should: nil,
		Not:    nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	gaia.PrettyPrint(res)

}
