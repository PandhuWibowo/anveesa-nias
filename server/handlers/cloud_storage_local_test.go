package handlers

// Scratch integration test against a local MinIO instance — exercises the
// real S3 wire logic (signing, delimiter-based listing, copy, batch delete)
// without touching any real cloud provider or the app's own database.
// Not meant to be committed / run in CI: requires MinIO running on
// localhost:19000 with the bucket seeded (see test setup commands).
//
// Run: MINIO_TEST=1 go test ./handlers/ -run TestLocalMinio -v

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func testDest() *bucketConnRow {
	return &bucketConnRow{
		Driver:   "s3_aws",
		Host:     "localhost",
		Port:     19000,
		Bucket:   "nias-test-bucket",
		Username: "testaccesskey",
		Password: "testsecretkey12345",
		SSL:      false,
	}
}

func TestLocalMinio(t *testing.T) {
	if os.Getenv("MINIO_TEST") != "1" {
		t.Skip("set MINIO_TEST=1 to run against local MinIO")
	}
	ctx := context.Background()
	dest := testDest()

	t.Run("list root", func(t *testing.T) {
		// Runs first, but t.Run subtests within the same top-level Test share
		// bucket state and Go doesn't guarantee execution order beyond source
		// order for non-parallel subtests — assert readme.txt/photos/ are
		// present rather than an exact match, since later subtests add more
		// root-level objects to the same bucket.
		files, folders, truncated, err := listBucketPage(ctx, dest, "")
		if err != nil {
			t.Fatalf("list root failed: %v", err)
		}
		t.Logf("files=%+v folders=%+v truncated=%v", files, folders, truncated)
		hasReadme := false
		for _, f := range files {
			if f.Key == "readme.txt" {
				hasReadme = true
			}
		}
		if !hasReadme {
			t.Errorf("expected readme.txt at root, got %+v", files)
		}
		hasPhotos := false
		for _, f := range folders {
			if f == "photos/" {
				hasPhotos = true
			}
		}
		if !hasPhotos {
			t.Errorf("expected photos/ folder at root, got %+v", folders)
		}
	})

	t.Run("list photos/ folder", func(t *testing.T) {
		files, folders, _, err := listBucketPage(ctx, dest, "photos/")
		if err != nil {
			t.Fatalf("list photos/ failed: %v", err)
		}
		t.Logf("files=%+v folders=%+v", files, folders)
		if len(files) != 1 || files[0].Key != "photos/other.txt" {
			t.Errorf("expected [photos/other.txt], got %+v", files)
		}
		if len(folders) != 1 || folders[0] != "photos/vacation/" {
			t.Errorf("expected [photos/vacation/] folder, got %+v", folders)
		}
	})

	t.Run("list nonexistent prefix", func(t *testing.T) {
		files, folders, _, err := listBucketPage(ctx, dest, "does-not-exist/")
		if err != nil {
			t.Fatalf("list nonexistent failed: %v", err)
		}
		if len(files) != 0 || len(folders) != 0 {
			t.Errorf("expected empty result for nonexistent prefix, got files=%+v folders=%+v", files, folders)
		}
	})

	t.Run("flat listBucketObjects under photos", func(t *testing.T) {
		objs, err := listBucketObjects(ctx, dest, "photos")
		if err != nil {
			t.Fatalf("listBucketObjects failed: %v", err)
		}
		if len(objs) != 3 {
			t.Errorf("expected 3 objects flat under photos/, got %d: %+v", len(objs), objs)
		}
	})

	t.Run("chunked upload rejected by strict S3 (documents why uploadObjectSpooled exists)", func(t *testing.T) {
		err := uploadToBucketStreamTyped(ctx, dest, "should-fail.txt", strings.NewReader("x"), "")
		if err == nil {
			t.Errorf("expected the chunked-transfer upload path to fail against MinIO (411 Length Required) — if this now passes, MinIO/Go behavior changed and uploadObjectSpooled may no longer be necessary")
		} else {
			t.Logf("confirmed: chunked upload rejected as expected: %v", err)
		}
	})

	t.Run("upload + download roundtrip with content-type", func(t *testing.T) {
		content := "hello from cloud storage test"
		if err := uploadObjectSpooled(ctx, dest, "test-upload.json", strings.NewReader(content), "application/json"); err != nil {
			t.Fatalf("upload failed: %v", err)
		}
		resp, err := openBucketObjectStream(ctx, dest, "test-upload.json", 0)
		if err != nil {
			t.Fatalf("download failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if string(body) != content {
			t.Errorf("downloaded content mismatch: got %q want %q", string(body), content)
		}
		ct := resp.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json preserved, got %q", ct)
		}
	})

	t.Run("delete single object", func(t *testing.T) {
		if err := uploadObjectSpooled(ctx, dest, "to-delete.txt", strings.NewReader("x"), ""); err != nil {
			t.Fatalf("setup upload failed: %v", err)
		}
		if err := deleteBucketObject(ctx, dest, "to-delete.txt"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}
		files, _, _, err := listBucketPage(ctx, dest, "")
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		for _, f := range files {
			if f.Key == "to-delete.txt" {
				t.Errorf("to-delete.txt still present after delete")
			}
		}
	})

	t.Run("copy object (rename mechanism)", func(t *testing.T) {
		if err := uploadObjectSpooled(ctx, dest, "copy-src.txt", strings.NewReader("copy me"), ""); err != nil {
			t.Fatalf("setup upload failed: %v", err)
		}
		if err := copyBucketObject(ctx, dest, "copy-src.txt", "copy-dst.txt"); err != nil {
			t.Fatalf("copy FAILED — x-amz-copy-source format may be wrong: %v", err)
		}
		resp, err := openBucketObjectStream(ctx, dest, "copy-dst.txt", 0)
		if err != nil {
			t.Fatalf("reading copy destination failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "copy me" {
			t.Errorf("copy destination content mismatch: got %q", string(body))
		}
		// Source should still exist (copy, not move)
		respSrc, err := openBucketObjectStream(ctx, dest, "copy-src.txt", 0)
		if err != nil {
			t.Errorf("copy source should still exist after copy: %v", err)
		} else {
			respSrc.Body.Close()
		}
	})

	t.Run("copy object with special characters in key", func(t *testing.T) {
		srcKey := "special chars/file with spaces & (parens)+plus.txt"
		dstKey := "special chars/renamed & moved.txt"
		if err := uploadObjectSpooled(ctx, dest, srcKey, strings.NewReader("special"), ""); err != nil {
			t.Fatalf("setup upload with special chars failed: %v", err)
		}
		if err := copyBucketObject(ctx, dest, srcKey, dstKey); err != nil {
			t.Errorf("copy with special characters FAILED: %v", err)
		} else {
			resp, err := openBucketObjectStream(ctx, dest, dstKey, 0)
			if err != nil {
				t.Errorf("reading copy-with-special-chars destination failed: %v", err)
			} else {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if string(body) != "special" {
					t.Errorf("special-char copy content mismatch: got %q", string(body))
				}
			}
		}
	})

	t.Run("batch delete many objects (spans batch boundary)", func(t *testing.T) {
		const n = 1200 // > 1000, forces batchDeleteBucketObjects to make 2 calls
		var keys []string
		for i := 0; i < n; i++ {
			k := fmt.Sprintf("bulk/obj-%04d.txt", i)
			if err := uploadObjectSpooled(ctx, dest, k, strings.NewReader("x"), ""); err != nil {
				t.Fatalf("setup upload %d failed: %v", i, err)
			}
			keys = append(keys, k)
		}
		deleted, err := batchDeleteBucketObjects(ctx, dest, keys)
		if err != nil {
			t.Fatalf("batch delete failed: %v", err)
		}
		if deleted != n {
			t.Errorf("expected %d deleted, got %d", n, deleted)
		}
		objs, err := listBucketObjects(ctx, dest, "bulk")
		if err != nil {
			t.Fatalf("verify list failed: %v", err)
		}
		if len(objs) != 0 {
			t.Errorf("expected 0 objects left under bulk/ after batch delete, got %d", len(objs))
		}
	})

	t.Run("mkdir marker then list shows folder", func(t *testing.T) {
		if err := uploadObjectSpooled(ctx, dest, "newfolder/", bytes.NewReader(nil), ""); err != nil {
			t.Fatalf("mkdir upload failed: %v", err)
		}
		_, folders, _, err := listBucketPage(ctx, dest, "")
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		found := false
		for _, f := range folders {
			if f == "newfolder/" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected newfolder/ to appear in root folder listing, got %+v", folders)
		}
		// Listing inside the new empty folder should show nothing (marker itself excluded)
		files, subfolders, _, err := listBucketPage(ctx, dest, "newfolder/")
		if err != nil {
			t.Fatalf("list newfolder/ failed: %v", err)
		}
		if len(files) != 0 || len(subfolders) != 0 {
			t.Errorf("expected empty listing inside newfolder/ (marker should be excluded), got files=%+v folders=%+v", files, subfolders)
		}
	})

	t.Run("delete nonexistent object does not error", func(t *testing.T) {
		if err := deleteBucketObject(ctx, dest, "does/not/exist.txt"); err != nil {
			t.Errorf("deleting nonexistent object should be a no-op (S3 semantics), got error: %v", err)
		}
	})

	t.Run("upload zero-byte file", func(t *testing.T) {
		if err := uploadObjectSpooled(ctx, dest, "zero-byte.txt", bytes.NewReader(nil), ""); err != nil {
			t.Fatalf("zero-byte upload failed: %v", err)
		}
		resp, err := openBucketObjectStream(ctx, dest, "zero-byte.txt", 0)
		if err != nil {
			t.Fatalf("zero-byte download failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if len(body) != 0 {
			t.Errorf("expected 0 bytes, got %d", len(body))
		}
	})

	t.Run("unicode filename roundtrip", func(t *testing.T) {
		key := "unicode/日本語ファイル名 🎉.txt"
		if err := uploadObjectSpooled(ctx, dest, key, strings.NewReader("unicode content"), ""); err != nil {
			t.Fatalf("unicode upload failed: %v", err)
		}
		resp, err := openBucketObjectStream(ctx, dest, key, 0)
		if err != nil {
			t.Fatalf("unicode download failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "unicode content" {
			t.Errorf("unicode content mismatch: got %q", string(body))
		}
		files, _, _, err := listBucketPage(ctx, dest, "unicode/")
		if err != nil {
			t.Fatalf("unicode list failed: %v", err)
		}
		if len(files) != 1 || files[0].Key != key {
			t.Errorf("expected unicode key in listing, got %+v", files)
		}
	})
}
