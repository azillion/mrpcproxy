package sdk

import (
	"fmt"
	"reflect"
	"testing"
)

func TestParseMappingCases(t *testing.T) {
	cases := []struct {
		json      []byte
		eps       []Endpoint
		expectErr bool
		err       error
	}{
		{
			[]byte(`
				{
					"/get/a/": {
						"endpoints": [
							{
								"topic": "a",
								"method": "GET",
								"keepAlive": 0
							}
						]
					},
					"/get/b/": {
						"endpoints": [
							{
								"topic": "b",
								"method": "GET",
								"keepAlive": 0
							}
						]
					},
					"/post/b/": {
						"endpoints": [
							{
								"topic": "b",
								"method": "POST",
								"keepAlive": 0
							}
						]
					},
					"/put/a/": {
						"endpoints": [
							{
								"topic": "a",
								"method": "PUT",
								"keepAlive": 0
							}
						]
					}
				}
			`),
			[]Endpoint{
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
