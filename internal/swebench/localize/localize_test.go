package localize

import "testing"

func TestParseTraceback(t *testing.T) {
	txt := "Traceback (most recent call last):\n  File \"django/db/models/query.py\", line 42, in get\n    x\n  File \"/usr/lib/python3/x.py\", line 7, in <module>\n"
	got := ParseTraceback(txt)
	if len(got) != 2 || got[0].File != "django/db/models/query.py" || got[0].Line != 42 || got[0].Function != "get" || got[1].Function != "<module>" {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractSymbols(t *testing.T) {
	got := ExtractSymbols("Calling `QuerySet.bulk_create()` on ModelAdmin breaks get_field_name")
	want := map[string]bool{"QuerySet.bulk_create": true, "ModelAdmin": true, "get_field_name": true}
	for _, s := range got {
		delete(want, s)
	}
	if len(want) != 0 {
		t.Fatalf("missing %v in %v", want, got)
	}
}
