package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/inttesting/integrationtest"
	"github.com/google/fleetspeak/fleetspeak/src/server/dbtesting"

	// We access the driver through sql.Open, but need to bring in the
	// dependency.
	_ "github.com/go-sql-driver/mysql"
)

var user = os.Getenv("MYSQL_TEST_USER")
var pass = os.Getenv("MYSQL_TEST_PASS")
var addr = os.Getenv("MYSQL_TEST_ADDR")

func setup(t *testing.T, caseName string) (ds *Datastore, fin func()) {
	if user == "" {
		t.Skip("MYSQL_TEST_USER not set")
	}
	if addr == "" {
		t.Skip("MYSQL_TEST_ADDR not set")
	}
	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
	defer fin()

	cs := fmt.Sprintf("%s:%s@tcp(%s)/", user, pass, addr)
	ac, err := sql.Open("mysql", cs)
	if err != nil {
		t.Fatalf("Unable to open connection [%s] to create database: %v", cs, err)
	}
	if _, err := ac.ExecContext(ctx, "CREATE DATABASE "+caseName); err != nil {
		t.Fatalf("Unable to create database [%s]: %v", caseName, err)
	}

	cs = fmt.Sprintf("%s:%s@tcp(%s)/%s", user, pass, addr, caseName)
	c, err := sql.Open("mysql", cs)
	if err != nil {
		t.Fatalf("Unable to open connection [%s] to database: %v", cs, err)
	}
	s, err := MakeDatastore(c)
	if err != nil {
		t.Fatal(err)
	}

	return s, func() {
		ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
		defer fin()
		if _, err := ac.ExecContext(ctx, "DROP DATABASE "+caseName); err != nil {
			t.Errorf("Unable to drop database [%s]: %v", caseName, err)
		}
		ac.Close()
	}
}

func TestClientStore(t *testing.T) {
	ds, fin := setup(t, "TestClientStore")
	defer fin()

	dbtesting.ClientStoreTest(t, ds)
	ds.Close()
}

func TestMessageStore(t *testing.T) {
	ms, fin := setup(t, "TestMessageStore")
	defer fin()

	dbtesting.MessageStoreTest(t, ms)
	ms.Close()
}

func TestBroadcastStore(t *testing.T) {
	ms, fin := setup(t, "TestBroadcastStore")
	defer fin()

	dbtesting.BroadcastStoreTest(t, ms)
	ms.Close()
}

func TestFileStore(t *testing.T) {
	ms, fin := setup(t, "TestFileStore")
	defer fin()

	dbtesting.FileStoreTest(t, ms)
	ms.Close()
}

func TestIntegration(t *testing.T) {
	tmpDir, tmpDirCleanup := comtesting.GetTempDir("mysql_integration")
	defer tmpDirCleanup()

	ms, fin := setup(t, "TestIntegration")
	defer fin()

	integrationtest.FRRIntegrationTest(t, ms, tmpDir)
}

func TestCloneHandling(t *testing.T) {
	tmpDir, tmpDirCleanup := comtesting.GetTempDir("mysql_clone_handling")
	defer tmpDirCleanup()

	ms, fin := setup(t, "TestCloneHandling")
	defer fin()

	integrationtest.CloneHandlingTest(t, ms, tmpDir)
}
