package store

import (
	"path/filepath"
	"testing"
)

// TestUpdateMediaMetadata_只更新传入字段 未传的字段保持原值（部分更新语义）。
func TestUpdateMediaMetadata_只更新传入字段(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	m := &MediaFile{
		ID: "mf1", Path: "/d/a.mp4", Name: "a.mp4", Size: 100,
		Mime: "video/mp4", Kind: "video", Platform: "bilibili",
		Uploader: "原UP", Title: "原标题", Description: "原简介", Tags: "a,b",
	}
	if err := s.UpsertMediaFile(m); err != nil {
		t.Fatal(err)
	}

	title := "新标题"
	if err := s.UpdateMediaMetadata("mf1", MediaMetadataUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMediaFile("mf1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "新标题" {
		t.Fatalf("标题应更新为「新标题」，实际 %q", got.Title)
	}
	if got.Uploader != "原UP" || got.Description != "原简介" || got.Tags != "a,b" {
		t.Fatalf("未传的字段不应被改动，实际 %+v", got)
	}
}

// TestUpdateMediaMetadata_传空串即清空 传空串是「清空这个字段」，不是「不改」。
func TestUpdateMediaMetadata_传空串即清空(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	m := &MediaFile{ID: "mf2", Path: "/d/b.mp4", Name: "b.mp4", Size: 1, Description: "有简介", Tags: "x"}
	if err := s.UpsertMediaFile(m); err != nil {
		t.Fatal(err)
	}
	empty := ""
	if err := s.UpdateMediaMetadata("mf2", MediaMetadataUpdate{Description: &empty, Tags: &empty}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMediaFile("mf2")
	if got.Description != "" || got.Tags != "" {
		t.Fatalf("传空串应清空字段，实际 description=%q tags=%q", got.Description, got.Tags)
	}
}

// TestUpdateMediaCover_封面路径可读写。
func TestUpdateMediaCover_封面路径可读写(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	m := &MediaFile{ID: "mf3", Path: "/d/c.mp4", Name: "c.mp4", Size: 1}
	if err := s.UpsertMediaFile(m); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMediaCover("mf3", "/d/c.cover.jpg"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMediaFile("mf3")
	if got.CoverPath != "/d/c.cover.jpg" {
		t.Fatalf("封面路径应可读写，实际 %q", got.CoverPath)
	}
}

// TestUpdateMediaMetadata_不存在报错 更新不存在的记录要报错而不是静默成功。
func TestUpdateMediaMetadata_不存在报错(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	title := "x"
	if err := s.UpdateMediaMetadata("not-exist", MediaMetadataUpdate{Title: &title}); err == nil {
		t.Fatal("更新不存在的记录应报错")
	}
}
