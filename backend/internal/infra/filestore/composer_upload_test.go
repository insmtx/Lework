package filestore

import "testing"

func TestValidateComposerUploadFilename(t *testing.T) {
	allowed := []string{
		"report.pdf",
		"notes.docx",
		"sheet.XLSX",
		"slide.ppt",
		"readme.md",
		"page.html",
		"legacy.htm",
		"photo.JPG",
		"photo.jpeg",
		"photo.png",
		"animation.gif",
		"bitmap.bmp",
		"photo.webp",
		"vector.svg",
		"plain.txt",
		"folder/sub/file.pdf",
	}
	for _, name := range allowed {
		if err := ValidateComposerUploadFilename(name); err != nil {
			t.Fatalf("expected %q to be allowed, got %v", name, err)
		}
	}

	rejected := []string{
		"archive.zip",
		"py.typed",
		"no-extension",
		"",
		"movie.mp4",
		"movie.mov",
		"movie.avi",
	}
	for _, name := range rejected {
		if err := ValidateComposerUploadFilename(name); err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}
