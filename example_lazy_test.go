package jsonflat_test

import (
	"fmt"
	"sync"

	"github.com/nayan9229/jsonflat"
)

// lazy compiles the config on first use and hands every later caller the
// same Transformer and error. This is the whole of what a program needs for
// a config that is read from disk or a flag at run time; a config that is
// part of the program is simpler still as a package-level
// jsonflat.MustCompile, which runs at init.
var lazy = sync.OnceValues(func() (*jsonflat.Transformer, error) {
	config := []byte(`{"keys": {"normalize": "snake"}}`) // os.ReadFile in a real program
	return jsonflat.Compile(config)
})

func Example_lazyCompile() {
	t, err := lazy()
	if err != nil {
		fmt.Println("config:", err)
		return
	}
	out, err := t.Transform([]byte(`{"userId": 7, "orderTotal": 9.5}`))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(out))
	// Output: {"user_id":7,"order_total":9.5}
}
