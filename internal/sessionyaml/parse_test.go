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

func TestParsePreservesRaceWhenIdentityTextIsDamaged(t *testing.T) {
	doc := []byte("WeekendInfo:\n  SubSessionID: 123\n" +
		"SessionInfo:\n  Sessions:\n    - SessionNum: 2\n      SessionType: Race\n" +
		"DriverInfo:\n  DriverCarIdx: 0\n  Drivers:\n" +
		"    - CarIdx: 0\n      UserName: 卫青\n" +
		"    - CarIdx: 1\n      UserName: @ unreadable\n      AbbrevName: 卫, \xe6\n" +
		"    - CarIdx: 2\n      UserName: \"Already Quoted\"\n" +
		"    - CarIdx: 3\n      UserName: Jos\xe9\n")

	info, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse race with damaged identity text: %v", err)
	}
	if !info.HasRaceSession() {
		t.Fatal("race session was lost while recovering damaged identity text")
	}
	if got := info.DriverInfo.Drivers[0].UserName; got != "卫青" {
		t.Errorf("valid UTF-8 UserName = %q, want %q", got, "卫青")
	}
	if got := info.DriverInfo.Drivers[1].UserName; got != "@ unreadable" {
		t.Errorf("damaged UserName = %q, want quoted literal", got)
	}
	if got := info.DriverInfo.Drivers[2].UserName; got != "Already Quoted" {
		t.Errorf("quoted UserName = %q, want quotes removed once", got)
	}
	if got := info.DriverInfo.Drivers[3].UserName; got != "José" {
		t.Errorf("Windows-1252 UserName = %q, want %q", got, "José")
	}
}

func TestNormalizeEncodingPreservesChineseBesideTruncation(t *testing.T) {
	doc := []byte("AbbrevName: 卫, \xe6\n")

	got, err := normalizeEncoding(doc)
	if err != nil {
		t.Fatalf("normalizeEncoding: %v", err)
	}
	if want := "AbbrevName: 卫, �\n"; string(got) != want {
		t.Errorf("normalized line = %q, want %q", got, want)
	}
}

func TestParseDoesNotHideStructuralDamage(t *testing.T) {
	doc := []byte("SessionInfo: [\nDriverInfo:\n  UserName: @ unreadable\n")

	if _, err := Parse(doc); err == nil {
		t.Fatal("Parse accepted structurally invalid session YAML")
	}
}
