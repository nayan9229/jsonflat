package jsonflat_test

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nayan9229/jsonflat"
	"github.com/nayan9229/jsonflat/presets"
)

// A preset is plain config. Unmarshal it, add your own key corrections, and
// build the Transformer.
func Example_rudderStackPreset() {
	var cfg jsonflat.Config
	if err := json.Unmarshal(presets.RudderStack, &cfg); err != nil {
		panic(err)
	}
	cfg.Keys.Aliases = []jsonflat.Alias{{To: "product_id", From: []string{"prodcutId", "pid"}}}
	cfg.Keys.Drop = []string{"context_ip"}
	t, err := jsonflat.New(cfg)
	if err != nil {
		panic(err)
	}

	body := []byte(`{"batch":[{
	  "type": "track", "event": "Product Added", "messageId": "m-1", "anonymousId": "anon-1",
	  "context": {"ip": "1.2.3.4", "app": {"name": "Shop"}},
	  "properties": {"prodcutId": "P1", "Total Price": 499.75, "user_id": "ignored"}
	}]}`)

	opt := jsonflat.Options{Now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}
	err = t.Each(body, opt, func(r *jsonflat.Record) error {
		if r.Err != nil {
			fmt.Println("bad record", r.Index, r.Err)
			return nil
		}
		// r.Fields is messageId, anonymousId, userId. Copy what you keep.
		fmt.Printf("%s key=%s -> %s\n", r.Name, r.Fields[1], r.JSON)
		return nil
	})
	if err != nil {
		panic(err)
	}
	// Output:
	// tracks key=anon-1 -> {"id":"m-1","anonymous_id":"anon-1","received_at":"2026-09-21T10:00:00.000Z","timestamp":"2026-09-21T10:00:00.000Z","event":"product_added","event_text":"Product Added","context_app_name":"Shop"}
	// product_added key=anon-1 -> {"id":"m-1","anonymous_id":"anon-1","received_at":"2026-09-21T10:00:00.000Z","timestamp":"2026-09-21T10:00:00.000Z","event":"product_added","event_text":"Product Added","context_app_name":"Shop","product_id":"P1","total_price":499.75}
}
