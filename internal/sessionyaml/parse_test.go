package sessionyaml

import "testing"

func TestParseDecodesWindows1252SessionInfo(t *testing.T) {
	doc := []byte("DriverInfo:\n  DriverCarIdx: 0\n  Drivers:\n    - CarIdx: 0\n      UserName: Jos\xe9\n")

	info, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse Windows-1252 document: %v", err)
	}
	if got := info.DriverInfo.Drivers[0].UserName; got != "José" {
		t.Errorf("UserName = %q, want %q", got, "José")
	}
}

func TestParseLeavesValidUTF8Unchanged(t *testing.T) {
	doc := []byte("DriverInfo:\n  DriverCarIdx: 0\n  Drivers:\n    - CarIdx: 0\n      UserName: Zoë\n")

	info, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse UTF-8 document: %v", err)
	}
	if got := info.DriverInfo.Drivers[0].UserName; got != "Zoë" {
		t.Errorf("UserName = %q, want %q", got, "Zoë")
	}
}

func TestParseQuotesInvalidIdentityPlaceholder(t *testing.T) {
	doc := []byte("DriverInfo:\n  DriverCarIdx: 0\n  Drivers:\n    - CarIdx: 0\n      UserName: ? ?\n      AbbrevName: \xe6\n      TeamName: ? ?\n")

	info, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse document with iRacing identity placeholder: %v", err)
	}
	if got := info.DriverInfo.Drivers[0].UserName; got != "? ?" {
		t.Errorf("UserName = %q, want %q", got, "? ?")
	}
}
