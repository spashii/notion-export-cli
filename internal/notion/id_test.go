package notion

import "testing"

func TestParseIDFromNotionPageURL(t *testing.T) {
	id, err := ParseID("https://www.notion.so/workspace/My-Page-37e16e1a4a5281e485dc002706287236?pvs=4")
	if err != nil {
		t.Fatal(err)
	}
	if id != "37e16e1a-4a52-81e4-85dc-002706287236" {
		t.Fatalf("unexpected id: %s", id)
	}
}

func TestParseIDIgnoresViewIDQuery(t *testing.T) {
	id, err := ParseID("https://www.notion.so/workspace/37e16e1a4a5281e485dc002706287236?v=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if id != "37e16e1a-4a52-81e4-85dc-002706287236" {
		t.Fatalf("unexpected id: %s", id)
	}
}

func TestNormalizeID(t *testing.T) {
	id := NormalizeID("37e16e1a4a5281e485dc002706287236")
	if id != "37e16e1a-4a52-81e4-85dc-002706287236" {
		t.Fatalf("unexpected id: %s", id)
	}
}
