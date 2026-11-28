package protocol

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestItemStackHasPatchCoversFields(t *testing.T) {
	if (ItemStack{ItemID: 1, Count: 3}).HasPatch() {
		t.Fatal("plain stack reports a patch")
	}
	typ := reflect.TypeFor[ItemStack]()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() || f.Name == "ItemID" || f.Name == "Count" {
			continue
		}
		var s ItemStack
		v := reflect.ValueOf(&s).Elem().Field(i)
		switch v.Kind() {
		case reflect.Map:
			m := reflect.MakeMap(v.Type())
			m.SetMapIndex(reflect.Zero(v.Type().Key()), reflect.New(v.Type().Elem()).Elem())
			v.Set(m)
		case reflect.Slice:
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		case reflect.Interface:
			v.Set(reflect.ValueOf(nbt.Compound{}))
		case reflect.Pointer:
			v.Set(reflect.New(v.Type().Elem()))
		default:
			setNonZero(v)
		}
		if !s.HasPatch() {
			t.Errorf("HasPatch ignores field %s", f.Name)
		}
	}
}

func setNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.Int8, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint64:
		v.SetUint(1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.String:
		v.SetString("x")
	case reflect.Array:
		for j := 0; j < v.Len(); j++ {
			setNonZero(v.Index(j))
		}
	case reflect.Struct:
		for _, field := range v.Fields() {
			if field.CanSet() {
				setNonZero(field)
				return
			}
		}
	}
}
