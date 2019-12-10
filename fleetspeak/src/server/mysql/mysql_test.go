package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/dbtesting"

	// We access the driver through sql.Open, but need to bring in the
	// dependency.
	_ "github.com/go-sql-driver/mysql"
)

// func setup(t *testing.T, caseName string) (ds *Datastore, fin func()) {
// 	if user == "" {
// 		t.Skip("MYSQL_TEST_USER not set")
// 	}
// 	if addr == "" {
// 		t.Skip("MYSQL_TEST_ADDR not set")
// 	}
// 	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
// 	defer fin()

// 	cs := fmt.Sprintf("%s:%s@tcp(%s)/", user, pass, addr)
// 	ac, err := sql.Open("mysql", cs)
// 	if err != nil {
// 		t.Fatalf("Unable to open connection [%s] to create database: %v", cs, err)
// 	}
// 	if _, err := ac.ExecContext(ctx, "DROP DATABASE IF EXISTS "+caseName); err != nil {
// 		t.Fatalf("Unable to drop database [%s]: %v", caseName, err)
// 	}
// 	if _, err := ac.ExecContext(ctx, "CREATE DATABASE "+caseName); err != nil {
// 		t.Fatalf("Unable to create database [%s]: %v", caseName, err)
// 	}

// 	cs = fmt.Sprintf("%s:%s@tcp(%s)/%s", user, pass, addr, caseName)
// 	c, err := sql.Open("mysql", cs)
// 	if err != nil {
// 		t.Fatalf("Unable to open connection [%s] to database: %v", cs, err)
// 	}
// 	s, err := MakeDatastore(c)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	return s, func() {
// 		ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
// 		defer fin()
// 		if _, err := ac.ExecContext(ctx, "DROP DATABASE "+caseName); err != nil {
// 			t.Errorf("Unable to drop database [%s]: %v", caseName, err)
// 		}	if user == "" {
// 			t.Skip("MYSQL_TEST_USER not set")
// 		}
// 		if addr == "" {
// 			t.Skip("MYSQL_TEST_ADDR not set")
// 		}
// 		ac.Close()
// 	}
// }

type mysqlTestEnv struct {
	user string
	pass string
	addr string

	dbName string

	aconn *sql.DB
	conn  *sql.DB
}

func (e *mysqlTestEnv) Create() error {
	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
	defer fin()

	cs := fmt.Sprintf("%s:%s@tcp(%s)/", e.user, e.pass, e.addr)
	var err error
	e.aconn, err = sql.Open("mysql", cs)
	if err != nil {
		return fmt.Errorf("Unable to open connection [%s] to create database: %v", cs, err)
	}
	if _, err = e.aconn.ExecContext(ctx, "DROP DATABASE IF EXISTS "+e.dbName); err != nil {
		return fmt.Errorf("Unable to drop database [%s]: %v", e.dbName, err)
	}
	if _, err = e.aconn.ExecContext(ctx, "CREATE DATABASE "+e.dbName); err != nil {
		return fmt.Errorf("Unable to create database [%s]: %v", e.dbName, err)
	}
	if _, err = e.aconn.ExecContext(ctx, "USE "+e.dbName); err != nil {
		return fmt.Errorf("Unable to use database [%s]: %v", e.dbName, err)
	}

	return nil
}

func (e *mysqlTestEnv) Clean() (db.Store, error) {
	if e.conn != nil {
		e.conn.Close()
	}

	ctx := context.Background()

	rows, err := e.aconn.QueryContext(ctx, "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE table_schema=?", e.dbName)
	if err != nil {
		return nil, fmt.Errorf("Can't fetch list of tables: %v", err)
	}

	tables := make([]string, 0)

	defer rows.Close()
	for rows.Next() {
		var tname string
		if err := rows.Scan(&tname); err != nil {
			return nil, err
		}

		tables = append(tables, tname)
	}

	if _, err = e.aconn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
		return nil, fmt.Errorf("Unable to disable foreign key checks: %v", err)
	}
	defer e.aconn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=1")

	for _, tname := range tables {
		if _, err = e.aconn.ExecContext(ctx, "TRUNCATE TABLE "+tname); err != nil {
			return nil, fmt.Errorf("Unable to truncate table %v: %v", tname, err)
		}
	}

	cs := fmt.Sprintf("%s:%s@tcp(%s)/%s", e.user, e.pass, e.addr, e.dbName)
	e.conn, err = sql.Open("mysql", cs)
	if err != nil {
		return nil, fmt.Errorf("Unable to open connection [%s] to database: %v", cs, err)
	}
	s, err := MakeDatastore(e.conn)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (e *mysqlTestEnv) Destroy() error {
	if e.conn != nil {
		e.conn.Close()
	}

	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
	defer fin()
	if _, err := e.aconn.ExecContext(ctx, "DROP DATABASE ?", e.dbName); err != nil {
		return fmt.Errorf("Unable to drop database [%s]: %v", e.dbName, err)
	}
	return e.aconn.Close()
}

func newMysqlTestEnv(user string, pass string, addr string) *mysqlTestEnv {
	return &mysqlTestEnv{
		user:   user,
		pass:   pass,
		addr:   addr,
		dbName: "fleetspeaktestdb",
	}
}

func TestMysqlStore(t *testing.T) {
	var user = os.Getenv("MYSQL_TEST_USER")
	var pass = os.Getenv("MYSQL_TEST_PASS")
	var addr = os.Getenv("MYSQL_TEST_ADDR")

	if user == "" {
		t.Skip("MYSQL_TEST_USER not set")
	}
	if addr == "" {
		t.Skip("MYSQL_TEST_ADDR not set")
	}

	dbtesting.DataStoreTestSuite(t, newMysqlTestEnv(user, pass, addr))
}
