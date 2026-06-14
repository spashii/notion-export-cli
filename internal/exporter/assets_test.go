package exporter

import (
	"net/url"
	"strings"
	"testing"
)

func TestCanonicalAssetURLStripsSignedQuery(t *testing.T) {
	raw := "https://prod-files-secure.s3.us-west-2.amazonaws.com/space/file/image.png?X-Amz-Date=20260613T205138Z&X-Amz-Signature=secret"
	downloadURL, canonicalURL, ok := canonicalAssetURL(raw)
	if !ok {
		t.Fatal("expected Notion asset URL")
	}
	if downloadURL != raw {
		t.Fatalf("unexpected download URL: %q", downloadURL)
	}
	want := "https://prod-files-secure.s3.us-west-2.amazonaws.com/space/file/image.png"
	if canonicalURL != want {
		t.Fatalf("unexpected canonical URL: %q", canonicalURL)
	}
}

func TestCanonicalAssetURLUnwrapsFileSource(t *testing.T) {
	source := "https://s3-us-west-2.amazonaws.com/secure.notion-static.com/abc/file.pdf?X-Amz-Signature=secret"
	wrapper := `{"source":"` + source + `","permissionRecord":{"table":"block"}}`
	raw := "file://" + url.PathEscape(wrapper)
	_, canonicalURL, ok := canonicalAssetURL(raw)
	if !ok {
		t.Fatal("expected wrapped Notion file URL")
	}
	want := "https://s3-us-west-2.amazonaws.com/secure.notion-static.com/abc/file.pdf"
	if canonicalURL != want {
		t.Fatalf("unexpected canonical URL: %q", canonicalURL)
	}
}

func TestRewriteAssetURLs(t *testing.T) {
	content := `![](https://prod-files-secure.s3.us-west-2.amazonaws.com/space/file/image.png?sig=1)
<pdf src="file://%7B%22source%22%3A%22https%3A%2F%2Fs3-us-west-2.amazonaws.com%2Fsecure.notion-static.com%2Fabc%2Ffile.pdf%22%7D"></pdf>
![external](https://example.com/image.png)`

	got := rewriteAssetURLs(content, func(raw string) (string, bool) {
		if strings.HasPrefix(raw, "https://prod-files-secure") {
			return "../extracted_assets/image.png", true
		}
		if strings.HasPrefix(raw, "file://") {
			return "../extracted_assets/file.pdf", true
		}
		return "", false
	})

	if !strings.Contains(got, `![](../extracted_assets/image.png)`) {
		t.Fatalf("markdown image was not rewritten: %s", got)
	}
	if !strings.Contains(got, `src="../extracted_assets/file.pdf"`) {
		t.Fatalf("HTML-ish source was not rewritten: %s", got)
	}
	if !strings.Contains(got, "https://example.com/image.png") {
		t.Fatalf("external URL should not have been rewritten: %s", got)
	}
}

func TestAssetFilenameAddsExtensionFromContentType(t *testing.T) {
	got := assetFilename("https://prod-files-secure.s3.us-west-2.amazonaws.com/space/file/asset", "image/png")
	if got != "asset.png" {
		t.Fatalf("unexpected filename: %q", got)
	}
}
