package skills

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"testing"
)

func createTestZip(files map[string]string) []byte {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, _ := w.Create(name)
		f.Write([]byte(content))
	}
	w.Close()
	return buf.Bytes()
}

func createMultipartFile(zipData []byte, filename string) (multipart.File, *multipart.FileHeader) {
	return &testFile{Reader: bytes.NewReader(zipData)}, &multipart.FileHeader{Filename: filename}
}

type testFile struct {
	*bytes.Reader
}

func (f *testFile) Close() error { return nil }

const validSkillMd = `---
name: test-skill
description: A test skill
---

This is a test skill.
`

func TestValidateZipSkill_RootLevel(t *testing.T) {
	zipData := createTestZip(map[string]string{
		"SKILL.md":          validSkillMd,
		"templates/foo.txt": "template content",
	})

	file, header := createMultipartFile(zipData, "test.zip")
	result, err := ValidateZipSkill("", file, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got error: %s", result.Error)
	}
	if result.Frontmatter.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", result.Frontmatter.Name)
	}
}

func TestValidateZipSkill_SingleFolderWrapper(t *testing.T) {
	zipData := createTestZip(map[string]string{
		"my-skill/SKILL.md":          validSkillMd,
		"my-skill/templates/foo.txt": "template content",
	})

	file, header := createMultipartFile(zipData, "test.zip")
	result, err := ValidateZipSkill("", file, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got error: %s", result.Error)
	}
	if result.Frontmatter.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", result.Frontmatter.Name)
	}
}

func TestValidateZipSkill_NoSkillMd(t *testing.T) {
	zipData := createTestZip(map[string]string{
		"README.md": "# Hello",
	})

	file, header := createMultipartFile(zipData, "test.zip")
	result, err := ValidateZipSkill("", file, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid, got valid")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestValidateZipSkill_InvalidExtension(t *testing.T) {
	file, header := createMultipartFile([]byte{}, "test.txt")
	result, err := ValidateZipSkill("", file, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid for non-zip file")
	}
}

func TestValidateZipSkill_InvalidFrontmatter(t *testing.T) {
	zipData := createTestZip(map[string]string{
		"SKILL.md": "no frontmatter here",
	})

	file, header := createMultipartFile(zipData, "test.zip")
	result, err := ValidateZipSkill("", file, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid for missing frontmatter")
	}
}

func TestFindSkillMdInZip_MultipleRootFolders(t *testing.T) {
	// When there are multiple root-level folders, it shouldn't find SKILL.md in nested folder
	zipData := createTestZip(map[string]string{
		"folder1/README.md": "readme",
		"folder2/SKILL.md":  validSkillMd,
	})

	buf := bytes.NewReader(zipData)
	reader, _ := zip.NewReader(buf, int64(len(zipData)))

	_, _, err := findSkillMdInZip(reader)
	if err == nil {
		t.Error("expected error for multiple root folders without root SKILL.md")
	}
}
