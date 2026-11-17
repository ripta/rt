package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// listedSchema is the subset of a JSON schema the nullability check reads.
type listedSchema struct {
	Type       any                      `json:"type"`
	Items      *listedSchema            `json:"items"`
	Properties map[string]*listedSchema `json:"properties"`
	Required   []string                 `json:"required"`
}

// TestRequiredArgumentsRejectNull checks every advertised tool schema as a client sees it.
// A required argument whose schema admits null tells callers that null is a valid value, so
// the call passes validation only to fail in the handler.
//
// It catches a new or changed tool whose required slice argument is registered through the
// reflected schema alone, which marks every Go slice nullable. It does not check optional
// arguments, where null and absence mean the same thing.
func TestRequiredArgumentsRejectNull(t *testing.T) {
	ctx := context.Background()
	server := newServer("test", time.Now(), "SESS", &gate{blindlyAllow: true}, nil)

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range res.Tools {
		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("%s: marshaling input schema: %v", tool.Name, err)
		}

		var schema listedSchema
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("%s: unmarshaling input schema: %v", tool.Name, err)
		}

		for _, name := range schema.Required {
			prop := schema.Properties[name]
			if prop == nil {
				t.Errorf("%s: required argument %q has no schema", tool.Name, name)
				continue
			}

			for path, s := name, prop; s != nil; path, s = path+"[]", s.Items {
				if schemaAllowsNull(s) {
					t.Errorf("%s: required argument %s admits null: %s", tool.Name, path, data)
				}
			}
		}
	}
}

func schemaAllowsNull(s *listedSchema) bool {
	switch typ := s.Type.(type) {
	case string:
		return typ == "null"
	case []any:
		return slices.Contains(typ, any("null"))
	}
	return false
}
