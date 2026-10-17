package store_test

import (
	"reflect"
	"testing"

	"github.com/moabukar/miniblue/internal/store"
	"github.com/moabukar/miniblue/internal/store/storetest"
)

type widget struct {
	Name  string            `json:"name"`
	Count int               `json:"count"`
	Tags  map[string]string `json:"tags,omitempty"`
}

func TestDecode(t *testing.T) {
	want := widget{Name: "w", Count: 3, Tags: map[string]string{"env": "dev"}}

	tests := []struct {
		name    string
		in      any
		want    widget
		wantErr bool
	}{
		{"struct fast path", want, want, false},
		{"decoded map", map[string]any{"name": "w", "count": float64(3), "tags": map[string]any{"env": "dev"}}, want, false},
		{"nil", nil, widget{}, false},
		{"wrong shape", []any{"not", "a", "widget"}, widget{}, true},
		{"unencodable", make(chan int), widget{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.Decode[widget](tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDecodePointer(t *testing.T) {
	got, err := store.Decode[*widget](map[string]any{"name": "w"})
	if err != nil || got == nil || got.Name != "w" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestGetAs(t *testing.T) {
	st, restart := storetest.NewFile(t)
	want := widget{Name: "w", Count: 3}
	st.Set("w", want)
	st.Set("bad", []any{"x"})

	for _, phase := range []string{"live struct", "after restart"} {
		t.Run(phase, func(t *testing.T) {
			got, found, err := store.GetAs[widget](st, "w")
			if err != nil || !found || !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, found=%v, err=%v", got, found, err)
			}

			if _, found, err := store.GetAs[widget](st, "missing"); found || err != nil {
				t.Errorf("missing key: found=%v, err=%v", found, err)
			}

			if _, found, err := store.GetAs[widget](st, "bad"); !found || err == nil {
				t.Errorf("undecodable value: found=%v, err=%v", found, err)
			}
		})
		st = restart()
	}
}
