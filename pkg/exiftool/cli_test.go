package exiftool

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunCLIWithIOStreamsAndExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code, err := RunCLIWithIO(context.Background(), []string{"-s3", "-FileType", filepath.Join("testdata", "test.pdf")}, nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunCLIWithIO failed: %v", err)
	}
	if code != 0 || strings.TrimSpace(stdout.String()) != "PDF" {
		t.Errorf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code, err = RunCLIWithIO(context.Background(), []string{"missing-file.jpg"}, nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunCLIWithIO failed: %v", err)
	}
	if code != 1 || !strings.Contains(stderr.String(), "File not found") {
		t.Errorf("exit=%d stderr=%q, want exit 1 with File not found", code, stderr.String())
	}
}

func TestRunCLIWithStdinArgfile(t *testing.T) {
	args := "-s3\n-Author\n" + filepath.Join("testdata", "test.pdf") + "\n"
	stdout, stderrMsg, code, err := RunCLIWithStdin(context.Background(), []string{"-@", "-"}, strings.NewReader(args))
	if err != nil {
		t.Fatalf("RunCLIWithStdin failed: %v", err)
	}
	if code != 0 {
		t.Errorf("exit=%d stderr=%q", code, stderrMsg)
	}
	t.Logf("stdout=%q", stdout)
}

func TestRunCLIArgumentsAreNotCode(t *testing.T) {
	for _, arg := range []string{"'", `\`, `'); die("x"); ('`, "日本語", `"; exit 3; "`} {
		stdout, stderrMsg, code, err := RunCLI([]string{"-echo", arg})
		if err != nil {
			t.Fatalf("RunCLI(%q) failed: %v", arg, err)
		}
		if code != 0 || strings.TrimRight(stdout, "\n") != arg {
			t.Errorf("-echo %q: exit=%d stdout=%q stderr=%q", arg, code, stdout, stderrMsg)
		}
	}
}

func TestCollectCLIMounts(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "new", "out.jpg")
	mounts := collectCLIMounts([]string{
		"relative.jpg",
		dir,
		"-o", missing,
		"-Artist<=" + filepath.Join(dir, "artist.txt"),
		"/",
	})
	if len(mounts) != 1 || mounts[0] != dir {
		t.Errorf("mounts = %v, want [%s]", mounts, dir)
	}
}

func TestReadMetadataTags(t *testing.T) {
	et := shared(t)
	src := filepath.Join("testdata", "test.jpg")

	tags, err := et.ReadMetadataTags(src, ReadOptions{Tags: []string{"FileType", "ImageWidth"}})
	if err != nil {
		t.Fatalf("ReadMetadataTags failed: %v", err)
	}
	got := map[string]TagInfo{}
	for _, tag := range tags {
		got[tag.Name] = tag
	}
	if len(got) != 2 || got["FileType"].Value != "JPEG" || got["FileType"].Group != "File" {
		t.Errorf("tags = %+v", tags)
	}

	tags, err = et.ReadMetadataTags(src, ReadOptions{Exclude: []string{"-FileType", "MIMEType"}})
	if err != nil {
		t.Fatalf("ReadMetadataTags failed: %v", err)
	}
	for _, tag := range tags {
		if tag.Name == "FileType" || tag.Name == "MIMEType" {
			t.Errorf("excluded tag %s was returned", tag.Name)
		}
	}
	if len(tags) == 0 {
		t.Error("expected the remaining tags")
	}
}

func TestGuestPathWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		if got := guestPath("/tmp/a b/c.jpg"); got != "/tmp/a b/c.jpg" {
			t.Errorf("guestPath changed a Unix path: %q", got)
		}
		return
	}
	if got := guestPath(`C:\Users\me\a.jpg`); got != "/C/Users/me/a.jpg" {
		t.Errorf("guestPath = %q", got)
	}
	args := guestArgs([]string{`-Comment=C:\keep\this`, `-Artist<=C:\x\artist.txt`, `C:\photos\a.jpg`, "-ver"})
	want := []string{`-Comment=C:\keep\this`, `-Artist<=/C/x/artist.txt`, "/C/photos/a.jpg", "-ver"}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("guestArgs[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}
