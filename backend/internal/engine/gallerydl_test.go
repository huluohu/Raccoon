package engine

import "testing"

// TestGallerySkipPath covers gallery-dl's "file exists, skipped" output lines.
//
// Why this matters: gallery-dl skips already-existing files and exits 0. The
// download collector diffs the destination directory for new files, so a fully
// skipped run produces nothing and gets misreported as NO_OUTPUT ("exited
// successfully but no artifact found"). Repeating the same gallery download then
// looks like a failure to the user even though every file is already local.
func TestGallerySkipPath(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{
			name: "skipped file line",
			line: "# ./flickr/Frank Romero (Frankxx)/flickr_49907674328.jpg",
			want: "./flickr/Frank Romero (Frankxx)/flickr_49907674328.jpg",
		},
		{
			name: "downloaded file line is not a skip line",
			line: "./flickr/Frank Romero (Frankxx)/flickr_49907674328.jpg",
			want: "",
		},
		{
			name: "progress line is not a skip line",
			line: "PROGRESS|3|102400|51200|1",
			want: "",
		},
		{
			name: "info log line is not a skip line",
			line: "[flickr][info] Retrieving public API key",
			want: "",
		},
		{
			name: "bare hash is not a skip line",
			line: "#",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gallerySkipPath(c.line); got != c.want {
				t.Fatalf("gallerySkipPath(%q) = %q, want %q", c.line, got, c.want)
			}
		})
	}
}
