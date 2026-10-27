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

// TestUpsertMediaFile_不覆盖用户编辑的元数据 重新登记同一路径时（如重新下载
// 同一文件、格式转换产物入库），worker.finish 传的 title/cover_path 是空的——
// 直接覆盖会把用户在媒体库整理好的标题与封面清空（2026-10-25 定位的 P0）。
func TestUpsertMediaFile_不覆盖用户编辑的元数据(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 用户手动整理过元数据
	if err := s.UpsertMediaFile(&MediaFile{
		ID: "mf9", Path: "/d/v.mp4", Name: "v.mp4", Size: 100,
		Kind: "video", Title: "我改过的标题", CoverPath: "/d/v.cover.jpg",
		Description: "我的简介", Tags: "科普,宇宙",
	}); err != nil {
		t.Fatal(err)
	}

	// 重新下载同一文件 → 引擎产物登记（title/cover 全空）
	if err := s.UpsertMediaFile(&MediaFile{
		ID: "mf10", Path: "/d/v.mp4", Name: "v.mp4", Size: 200,
		Kind: "video", Platform: "bilibili", Uploader: "某UP",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetMediaFile("mf9")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "我改过的标题" {
		t.Fatalf("标题不应被覆盖，实际 %q", got.Title)
	}
	if got.CoverPath != "/d/v.cover.jpg" {
		t.Fatalf("封面不应被覆盖，实际 %q", got.CoverPath)
	}
	if got.Description != "我的简介" || got.Tags != "科普,宇宙" {
		t.Fatalf("简介/标签不应被覆盖，实际 %q / %q", got.Description, got.Tags)
	}
	// 引擎侧的新值仍要写入
	if got.Size != 200 || got.Uploader != "某UP" {
		t.Fatalf("引擎侧字段应更新，实际 size=%d uploader=%q", got.Size, got.Uploader)
	}
}
