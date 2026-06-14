package codegen

import (
	"testing"

	"github.com/andrioid/statesman/schema"
)

func TestWarningsPromiseNoExit(t *testing.T) {
	promise := func() *Resolution {
		return &Resolution{Actors: map[string]*ActorSym{"fetch": {Src: "fetch", Kind: AdapterPromise}}}
	}
	cases := []struct {
		name string
		json string
		res  *Resolution
		want int
	}{
		{
			name: "bare promise invoke warns (on-event is not an automatic exit)",
			json: `{"id":"w","initial":"a","states":{"a":{"invoke":[{"id":"f","src":"fetch"}],"on":{"X":{"target":"b"}}},"b":{"type":"final"}}}`,
			res:  promise(),
			want: 1,
		},
		{
			name: "onError suppresses",
			json: `{"id":"w","initial":"a","states":{"a":{"invoke":[{"id":"f","src":"fetch","onError":{"target":"b"}}]},"b":{"type":"final"}}}`,
			res:  promise(),
			want: 0,
		},
		{
			name: "after on same state suppresses",
			json: `{"id":"w","initial":"a","states":{"a":{"invoke":[{"id":"f","src":"fetch"}],"after":{"1000":{"target":"b"}}},"b":{"type":"final"}}}`,
			res:  promise(),
			want: 0,
		},
		{
			name: "after on ancestor suppresses",
			json: `{"id":"w","initial":"p","states":{"p":{"initial":"a","after":{"1000":{"target":"#b"}},"states":{"a":{"invoke":[{"id":"f","src":"fetch"}]}}},"b":{"type":"final"}}}`,
			res:  promise(),
			want: 0,
		},
		{
			name: "non-promise kind not warned",
			json: `{"id":"w","initial":"a","states":{"a":{"invoke":[{"id":"f","src":"fetch"}],"on":{"X":{"target":"b"}}},"b":{"type":"final"}}}`,
			res:  &Resolution{Actors: map[string]*ActorSym{"fetch": {Src: "fetch", Kind: AdapterCallback}}},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def, err := schema.Load([]byte(tc.json))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			got := Warnings(tc.res, def)
			if len(got) != tc.want {
				t.Fatalf("warnings = %d %v, want %d", len(got), got, tc.want)
			}
		})
	}
}
