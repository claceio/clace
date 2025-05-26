// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/claceio/clace/internal/types"
)

const (
	DB_CONNECTION_CONFIG = "db_connection"
)

func SQLItePragmas(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=10000"); err != nil {
		return err
	}

	return nil
}

func CheckConnectString(connStr string, invoker string, supportedDBs []string) (string, string, error) {
	parts := strings.SplitN(connStr, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid connection string: %s", connStr)
	}

	if !slices.Contains(supportedDBs, parts[0]) {
		return "", "", fmt.Errorf("invalid database type: %s, only %s supported for %s", parts[0], strings.Join(supportedDBs, ", "), invoker)
	}

	return parts[0], os.ExpandEnv(parts[1]), nil
}

const (
	DB_TYPE_SQLITE   = "sqlite"
	DB_TYPE_POSTGRES = "postgres"
)

var (
	DB_SQLITE_POSTGRES = []string{DB_TYPE_SQLITE, DB_TYPE_POSTGRES}
	DB_SQLITE          = []string{DB_TYPE_SQLITE}
	DRIVER_MAP         = map[string]string{
		DB_TYPE_SQLITE:   "sqlite",
		DB_TYPE_POSTGRES: "pgx",
	}
)

func InitDBConnection(connectString string, invoker string, supportedDBs []string) (*sql.DB, error) {
	var err error
	dbType, connectString, err := CheckConnectString(connectString, invoker, supportedDBs)
	if err != nil {
		return nil, err
	}

	driver := DRIVER_MAP[dbType]
	if driver == "" {
		return nil, fmt.Errorf("unknown database type: %s", dbType)
	}

	db, err := sql.Open(driver, connectString)
	if err != nil {
		return nil, fmt.Errorf("error opening %s db %s: %w", invoker, connectString, err)
	}

	if dbType == DB_TYPE_SQLITE {
		if err := SQLItePragmas(db); err != nil {
			return nil, err
		}
	}
	return db, nil
}

func GetConnectString(pluginContext *types.PluginContext) (string, error) {
	connectStringConfig, ok := pluginContext.Config[DB_CONNECTION_CONFIG]
	if !ok {
		return "", fmt.Errorf("db connection string not found in config")
	}
	connectString, ok := connectStringConfig.(string)
	if !ok {
		return "", fmt.Errorf("db connection string is not a string")
	}
	return connectString, nil
}
