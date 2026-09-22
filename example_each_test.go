package jsonflat_test

import (
	"fmt"

	"github.com/nayan9229/jsonflat"
)

// Each splits a document into records and routes every record to the outputs
// whose condition it meets. The callback sees one Record per row, with the
// output name, the exposed fields and the flat JSON. A record that fails is
// reported through Record.Err and does not stop the batch.
func ExampleTransformer_Each() {
	t := jsonflat.MustCompile([]byte(`{
	  "input":   {"explode": "items", "inherit": ["orderId"]},
	  "flatten": {"separator": "_"},
	  "keys":    {"normalize": "snake"},
	  "columns": [{"to": "order_id", "from": "orderId", "as": "string"}],
	  "outputs": [
	    {"name": "items"},
	    {"name": "gifts", "when": {"path": "gift", "equals": "true"}}
	  ],
	  "expose": ["sku"]
	}`))

	body := []byte(`{"orderId": 981, "items": [
	  {"sku": "A1", "unitPrice": 499.75},
	  {"sku": "B7", "unitPrice": 5, "gift": "true"},
	  "not a record"
	]}`)

	err := t.Each(body, jsonflat.Options{}, func(r *jsonflat.Record) error {
		if r.Err != nil {
			fmt.Printf("record %d failed: %v\n", r.Index, r.Err)
			return nil // one bad record does not fail the batch
		}
		// r.Name, r.Fields and r.JSON are valid until this function returns.
		fmt.Printf("%s sku=%s %s\n", r.Name, r.Fields[0], r.JSON)
		return nil
	})
	if err != nil {
		panic(err)
	}
	// Output:
	// items sku=A1 {"order_id":"981","sku":"A1","unit_price":499.75}
	// items sku=B7 {"order_id":"981","sku":"B7","unit_price":5,"gift":"true"}
	// gifts sku=B7 {"order_id":"981","sku":"B7","unit_price":5,"gift":"true"}
	// record 2 failed: jsonflat: record is not an object or an array
}

// A Config can be built in Go instead of parsed from JSON. New validates it
// the same way Compile does. A single source is a SourceList with one entry.
func ExampleNew() {
	cfg := jsonflat.Config{
		Flatten: jsonflat.FlattenConfig{Separator: "_", DropNulls: true},
		Keys: jsonflat.KeysConfig{
			Normalize:   jsonflat.NormalizeSnake,
			OnCollision: jsonflat.CollisionFirst,
			Aliases:     []jsonflat.Alias{{To: "product_id", From: []string{"prodcutId"}}},
			Keep:        []string{"id", "product_id", "price_*"},
		},
		Columns: []jsonflat.Column{
			{To: "id", From: jsonflat.SourceList{{Path: "messageId"}}, As: jsonflat.AsString},
		},
	}
	t, err := jsonflat.New(cfg)
	if err != nil {
		panic(err)
	}

	src := []byte(`{"messageId": 42, "prodcutId": "P1", "price": {"amount": 9.5, "currency": "EUR"}, "debug": null, "note": "dropped by keep"}`)
	out, err := t.Transform(src)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))
	// Output:
	// {"id":"42","product_id":"P1","price_amount":9.5,"price_currency":"EUR"}
}
