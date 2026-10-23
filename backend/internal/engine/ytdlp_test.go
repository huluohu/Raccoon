package engine

import "testing"

// TestAlreadyDownloadedPath covers yt-dlp's "file already exists" output lines.
//
// Why this matters: yt-dlp exits 0 without printing a "Destination:" line when
// the target file already exists. The download collector only understands
// "Destination:", so without recognizing these lines the task is misreported as
// NO_OUTPUT ("engine exited successfully but no artifact file found") even
// though the file is sitting right there in the download directory. Repeating
// the same download then looks like a failure to the user.
func TestAlreadyDownloadedPath(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{
			name: "plain already-downloaded line",
			line: "[download] /data/downloads/xxx [id].mp4 has already been downloaded",
			want: "/data/downloads/xxx [id].mp4",
		},
		{
			name: "already-downloaded and merged (DASH merge case)",
			line: "[download] /data/downloads/xxx [id].mkv has already been downloaded and merged",
			want: "/data/downloads/xxx [id].mkv",
		},
		{
			name: "relative path (relative -o template, cwd=SaveDir)",
			line: "[download] xxx [id].mp4 has already been downloaded",
			want: "xxx [id].mp4",
		},
		{
			name: "Destination line is not an already-downloaded line",
			line: "[download] Destination: xxx [id].mp4",
			want: "",
		},
		{
			name: "progress line is not an already-downloaded line",
			line: "[download]  43.0% of   10.00MiB at    5.00MiB/s ETA 00:01",
			want: "",
		},
		{
			name: "empty line",
			line: "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := alreadyDownloadedPath(c.line); got != c.want {
				t.Fatalf("alreadyDownloadedPath(%q) = %q, want %q", c.line, got, c.want)
			}
		})
	}
}
