package util

import (
	"fmt"
	"reflect"
)

func Override(dest, src interface{}) {
	p1 := reflect.ValueOf(dest)
	p2 := reflect.ValueOf(src)

	overrideValue(p1, p2)
}

func overrideValue(dest, src reflect.Value) {
	v1 := dest.Elem()
	v2 := src.Elem()

	t1 := v1.Type()
	t2 := v2.Type()

	if t1 != t2 {
		panic("src and dest must point to values of the same type")
	}

	switch t1.Kind() {
	case reflect.Struct:
		for ix := 0; ix < t1.NumField(); ix++ {
			overrideValue(v1.Field(ix).Addr(), v2.Field(ix).Addr())
		}
	case reflect.Slice:
		if (v1.IsZero() || v1.Len() == 0) && !v2.IsZero() {
			//fmt.Printf("%#v is zero and %#v is not zero\n", v1, v2)
			v1.Set(v2)
		} /*else {
			fmt.Printf("%#v is not zero or %#v is zero\n", v1, v2)
		} */
	case reflect.Map:
		if v2.IsZero() || v2.Len() == 0 {
			return
		}
		if v1.IsZero() {
			v1.Set(reflect.MakeMap(t1))
			fmt.Printf("v1: %#v %s\n", v1, t1)
		}
		iter := v2.MapRange()
		for iter.Next() {
			key := iter.Key()
			valueType := iter.Value().Type()

			tmpV1 := v1.MapIndex(key)
			tmpV2 := iter.Value()

			if tmpV1.Kind() == reflect.Invalid {
				//fmt.Printf("key %s is zero\n", key)
				v1.SetMapIndex(iter.Key(), tmpV2)
				//fmt.Printf("%#v\n", v1.Interface())
				continue
			}

			//fmt.Printf("merging key %s\n", key)
			tmpP1 := reflect.New(valueType)
			tmpP2 := reflect.New(valueType)
			tmpP1.Elem().Set(tmpV1)
			tmpP2.Elem().Set(tmpV2)
			overrideValue(tmpP1, tmpP2)
			v1.SetMapIndex(key, tmpP1.Elem())
		}
	default:
		if v1.IsZero() && !v2.IsZero() {
			//fmt.Printf("%#v is zero and %#v is not zero\n", v1, v2)
			v1.Set(v2)
		} /*else {
			fmt.Printf("%#v is not zero or %#v is zero\n", v1, v2)
		} */
	}
}
