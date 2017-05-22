package sdk

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/miracl/mrpcproxy"
)

func TestParseMappingCases(t *testing.T) {
	cases := []struct {
		json      []byte
		eps       []mrpcproxy.Endpoint
		expectErr bool
		err       error
	}{
		{
			[]byte(`
				{
				  "a": {
				    "endpoints": [
				      {
				        "method": "GET",
				        "path": "/get/a/"
				      },
				      {
				        "method": "PUT",
				        "path": "/put/a/"
				      }
				    ]
				  },
				  "b": {
				    "endpoints": [
				      {
				        "method": "GET",
				        "path": "/get/b/"
				      },
				      {
				        "method": "POST",
				        "path": "/post/b/"
				      }
				    ]
				  }
				}
			`),
			[]mrpcproxy.Endpoint{
				{Topic: "a", Method: "GET", Path: "/get/a/"},
				{Topic: "a", Method: "PUT", Path: "/put/a/"},
				{Topic: "b", Method: "GET", Path: "/get/b/"},
				{Topic: "b", Method: "POST", Path: "/post/b/"},
			},
			false,
			nil,
		},
		{
			[]byte("{}"),
			nil,
			true,
			ErrNoEndpoints,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("Case%v", i), func(t *testing.T) {
			eps, err := ParseMapping(tc.json)

			if err != nil && (!tc.expectErr || (tc.expectErr && err != ErrNoEndpoints)) {
				t.Fatalf("ParseMapping unexpected error: %v", err)
			}

			if err == nil && tc.expectErr {
				t.Fatal("ParseMapping expected error not returned")
			}

			if len(eps) != len(tc.eps) {
				t.Fatalf("Endpoints don't match\nExpected: %v\nReceived: %v", tc.eps, eps)
			}

			for expected := range tc.eps {
				found := false
				for received := range eps {
					if reflect.DeepEqual(expected, received) {
						found = true
						continue
					}
				}
				if !found {
					t.Fatalf("Endpoints don't match\nExpected: %v\nReceived: %v", tc.eps, eps)
				}
			}
		})
	}
}

func TestParseMappingCasesError(t *testing.T) {
	cases := []struct {
		json []byte
		err  string
	}{
		{
			[]byte(""),
			"error parsing endpoints: unexpected end of JSON input",
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("Case%v", i), func(t *testing.T) {
			_, err := ParseMapping(tc.json)

			switch err.(type) {
			case ParseError:
				if err.Error() != tc.err {
					t.Fatalf("Unexpected error: %v", err)
				}
			default:
				t.Fatalf("Unexpected error: %v", err)
			}

		})
	}
}
