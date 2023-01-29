package util_test

import (
	"testing"

	"github.com/meln5674/kink/pkg/config/util"
)

func TestString(t *testing.T) {
	x := ""
	y := "test"

	util.Override(&x, &y)
	if x != "test" {
		t.Error("empty string was not overridden")
	}

	x = "test"
	y = "test2"

	util.Override(&x, &y)
	if x != "test" {
		t.Error("non-empty string was overridden")
	}
}

func TestInt(t *testing.T) {
	x := 0
	y := 1

	util.Override(&x, &y)
	if x != 1 {
		t.Error("empty int was not overridden")
	}

	x = 1
	y = 2

	util.Override(&x, &y)
	if x != 1 {
		t.Error("non-empty int was overridden")
	}
}

func TestMap(t *testing.T) {
	x := map[string]string{}
	y := map[string]string{"foo": "bar"}

	util.Override(&x, &y)
	if x["foo"] != "bar" {
		t.Error("empty map field was not overridden")
	}

	x = map[string]string{"foo": "bar"}
	y = map[string]string{"foo": "baz"}

	util.Override(&x, &y)
	if x["foo"] != "bar" {
		t.Error("non-empty map field was overridden")
	}

	x = map[string]string{"foo": "baz"}
	y = map[string]string{"bar": "qux"}

	util.Override(&x, &y)
	if x["foo"] != "baz" || x["bar"] != "qux" {
		t.Errorf("map was not merged properly: %#v", x)
	}
}

type TestStructType struct {
	Foo string
	Bar string
}

func TestStruct(t *testing.T) {
	x := TestStructType{}
	y := TestStructType{Foo: "bar"}

	util.Override(&x, &y)
	if x.Foo != "bar" {
		t.Error("empty struct field was not overridden")
	}

	x = TestStructType{Foo: "bar"}
	y = TestStructType{Foo: "baz"}

	util.Override(&x, &y)
	if x.Foo != "bar" {
		t.Error("non-empty struct field was overridden")
	}

	x = TestStructType{Foo: "baz"}
	y = TestStructType{Bar: "qux"}

	util.Override(&x, &y)
	if x.Foo != "baz" || x.Bar != "qux" {
		t.Errorf("struct was not merged properly: %#v", x)
	}
}

func TestSlice(t *testing.T) {
	x := []string{}
	y := []string{"bar"}

	util.Override(&x, &y)
	if len(x) == 0 {
		t.Error("empty slice was not overridden")
	}
	if len(x) != 1 || x[0] != "bar" {
		t.Error("empty slice was overridden incorrectly")
	}

	x = []string{"bar"}
	y = []string{"baz"}

	util.Override(&x, &y)
	if len(x) == 0 {
		t.Error("non-empty slice was discarded")
	}
	if len(x) != 1 {
		t.Error("non-empty slice was appended")
	}
	if x[0] != "bar" {
		t.Error("non-empty struct field was overridden")
	}
}
