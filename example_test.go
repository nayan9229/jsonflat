package jsonflat_test

import (
	"fmt"

	"github.com/nayan9229/jsonflat"
)

func Example() {
	t := jsonflat.MustCompile([]byte(`{
	  "rules": [
	    {"op": "rename",  "from": "user.id", "to": "uid"},
	    {"op": "merge",   "from": ["user.first", "user.last"], "to": "user.name", "sep": " "},
	    {"op": "drop",    "path": "debug"},
	    {"op": "default", "path": "env", "value": "prod"}
	  ]
	}`))

	src := []byte(`{"user":{"id":7,"first":"Ada","last":"Lovelace"},"tags":["a","b"],"debug":{"x":1}}`)

	var out []byte // reuse this buffer between calls
	out, err := t.Append(out[:0], src)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))
	// Output:
	// {"uid":7,"tags.0":"a","tags.1":"b","user.name":"Ada Lovelace","env":"prod"}
}
