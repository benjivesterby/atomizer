package atomizer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/devnw/validator"
)

var pay = `{"test":"test"}`
var pay64Encoded = `eyJ0ZXN0IjoidGVzdCJ9`

var nonb64 = Electron{
	SenderID: "empty",
	ID:       "empty",
	AtomID:   "empty",
	Payload:  []byte(pay),
}

func TestElectron_MarshalJSON(t *testing.T) {

	tests := []struct {
		name     string
		e        Electron
		expected string
		err      bool
	}{
		{
			"valid electron",
			noopelectron,
			`{"senderid":"empty","id":"empty","atomid":"empty"}`,
			false,
		},
		{
			"valid electron w/ payload",
			nonb64,
			fmt.Sprintf(`{"senderid":"empty","id":"empty","atomid":"empty","payload":%s}`, pay),
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := json.Marshal(test.e)
			if err != nil && !test.err {
				t.Errorf("expected success, got error | %s", err.Error())
			}

			if err == nil && test.err {
				t.Error("expected error")
			}

			if strings.Compare(string(res), test.expected) != 0 {
				t.Errorf(
					"mismatch: e[%s] != r[%s]",
					test.expected,
					string(res),
				)
			}
		})
	}
}

func TestElectron_UnmarshalJSON(t *testing.T) {

	tests := []struct {
		name     string
		expected Electron
		json     string
		err      bool
	}{
		{
			"valid electron",
			noopelectron,
			`{"senderid":"empty","id":"empty","atomid":"empty"}`,
			false,
		},
		{
			"valid electron / non-base64 payload",
			nonb64,
			`{"senderid":"empty","id":"empty","atomid":"empty","payload":{"test":"test"}}`,
			false,
		},
		{
			"valid electron / base64 payload",
			nonb64,
			fmt.Sprintf(`{"senderid":"empty","id":"empty","atomid":"empty","payload":"%s"}`, pay64Encoded),
			false,
		},
		{
			"invalid json blob",
			Electron{},
			`{"empty"}`,
			true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := Electron{}
			err := json.Unmarshal([]byte(test.json), &e)

			if err != nil && !test.err {
				t.Errorf("expected success, got error | %s", err.Error())
			}

			if err == nil && test.err {
				t.Error("expected error")
			}

			if !reflect.DeepEqual(test.expected, e) {
				t.Errorf(
					"expected equality e[%s] != r[%s]",
					spew.Sdump(test.expected),
					spew.Sdump(e),
				)
			}
		})
	}
}

func TestElectron_Validate(t *testing.T) {

	tests := []struct {
		name  string
		e     Electron
		valid bool
	}{
		{
			"valid electron",
			noopelectron,
			true,
		},
		{
			"invalid electron",
			Electron{},
			false,
		},
		{
			"invalid electron / only sender",
			Electron{SenderID: "test"},
			false,
		},
		{
			"invalid electron / only atom",
			Electron{AtomID: "test"},
			false,
		},
		{
			"invalid electron / only ID",
			Electron{ID: "test"},
			false,
		},
		{
			"invalid electron / sender & atom",
			Electron{SenderID: "test", AtomID: "test"},
			false,
		},
		{
			"invalid electron / ID & sender",
			Electron{ID: "test", SenderID: "test"},
			false,
		},
		{
			"invalid electron / ID & atom",
			Electron{ID: "test", AtomID: "test"},
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !validator.Valid(test.e) == test.valid {
				t.Errorf("expected valid = %v", test.valid)
			}
		})
	}
}