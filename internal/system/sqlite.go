// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"database/sql"
	"fmt"
	"os"
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

func CheckConnectString(connStr string) (string, error) {
	parts := strings.SplitN(connStr, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid connection string: %s", connStr)
	}
	if !strings.HasPrefix(parts[0], "sqlite") {
		return "", fmt.Errorf("invalid connection string: %s, only sqlite supported", connStr)
	}
	return os.ExpandEnv(parts[1]), nil
}

func InitDB(connectString string) (*sql.DB, error) {
	var err error
	connectString, err = CheckConnectString(connectString)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", connectString)
	if err != nil {
		return nil, fmt.Errorf("error opening db %s: %w", connectString, err)
	}

	if err := SQLItePragmas(db); err != nil {
		return nil, err
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
