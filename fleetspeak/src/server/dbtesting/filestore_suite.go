package dbtesting

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
)

// FileStoreTest tests a FileStore.
func FileStoreTest(t *testing.T, fs db.Store) {
	ctx := context.Background()

	fakeTime := sertesting.FakeNow(84)
	defer fakeTime.Revert()

	data := []byte("The quick sly fox jumped over the lazy dogs.")

	if err := fs.StoreFile(ctx, "testService", "testFile", bytes.NewReader(data)); err != nil {
		t.Errorf("Error from StoreFile(testService, testFile): %v", err)
	}

	ts, err := fs.StatFile(ctx, "testService", "testFile")
	if err != nil {
		t.Errorf("Error from StatFile(testService, testFile): %v", err)
	}
	if ts != fakeTime.Get() {
		t.Errorf("Wrong result of StatfileFile(testService, testFile), want %v got %v:", fakeTime.Get(), ts)
	}

	res, ts, err := fs.ReadFile(ctx, "testService", "testFile")
	if err != nil {
		t.Fatalf("Error from ReadFile(testService, testFile): %v", err)
	}
	rb, err := ioutil.ReadAll(res)
	if err != nil {
		t.Errorf("Error reading result of ReadFile(testService, testFile): %v", err)
	}
	if c, ok := res.(io.Closer); ok {
		c.Close()
	}
	if !bytes.Equal(rb, data) || ts != fakeTime.Get() {
		t.Errorf("Wrong result of ReadFile(testService, testFile), want (%v, %v) got (%v, %v):",
			fakeTime.Get(), data, ts, rb)
	}

	if _, err := fs.StatFile(ctx, "testService", "missingFile"); err == nil || !fs.IsNotFound(err) {
		t.Errorf("Wrong error for ReadFile(testService, missingFile), want IsNotFound(err)=true, got %v", err)
	}
	if _, _, err := fs.ReadFile(ctx, "testService", "missingFile"); err == nil || !fs.IsNotFound(err) {
		t.Errorf("Wrong error for ReadFile(testService, missingFile), want IsNotFound(err)=true, got %v", err)
	}
}

func fileStoreTestSuite(t *testing.T, env DbTestEnv) {
	t.Run("FileStoreTestSuite", func(t *testing.T) {
		runTestSuite(t, env, map[string]func(*testing.T, db.Store){
			"FileStoreTest": FileStoreTest,
		})
	})
}
