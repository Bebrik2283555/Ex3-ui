package locale

import (
	"reflect"
	"testing"
)

func TestCreateTemplateDataSkipsParamWithoutSeparator(t *testing.T) {
	got := createTemplateData([]string{"Name==alice", "raw"}, "==")
	want := map[string]any{"Name": "alice"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("createTemplateData() = %v, want %v", got, want)
	}
}
