package cluster

// The code below was generated by lxd-generate - DO NOT EDIT!

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/lxc/lxd/lxd/db/query"
	"github.com/lxc/lxd/shared/api"
)

var _ = api.ServerEnvironment{}

var internalTokenRecordObjects = RegisterStmt(`
SELECT internal_token_records.id, internal_token_records.token, internal_token_records.joiner_cert
  FROM internal_token_records
  ORDER BY internal_token_records.joiner_cert
`)

var internalTokenRecordObjectsByJoinerCert = RegisterStmt(`
SELECT internal_token_records.id, internal_token_records.token, internal_token_records.joiner_cert
  FROM internal_token_records
  WHERE internal_token_records.joiner_cert = ? ORDER BY internal_token_records.joiner_cert
`)

var internalTokenRecordID = RegisterStmt(`
SELECT internal_token_records.id FROM internal_token_records
  WHERE internal_token_records.joiner_cert = ?
`)

var internalTokenRecordCreate = RegisterStmt(`
INSERT INTO internal_token_records (token, joiner_cert)
  VALUES (?, ?)
`)

var internalTokenRecordDeleteByJoinerCert = RegisterStmt(`
DELETE FROM internal_token_records WHERE joiner_cert = ?
`)

// GetInternalTokenRecordID return the ID of the internal_token_record with the given key.
// generator: internal_token_record ID
func GetInternalTokenRecordID(ctx context.Context, tx *sql.Tx, joinerCert string) (int64, error) {
	stmt := stmt(tx, internalTokenRecordID)
	rows, err := stmt.Query(joinerCert)
	if err != nil {
		return -1, fmt.Errorf("Failed to get \"internals_tokens_records\" ID: %w", err)
	}

	defer func() { _ = rows.Close() }()

	// Ensure we read one and only one row.
	if !rows.Next() {
		return -1, api.StatusErrorf(http.StatusNotFound, "InternalTokenRecord not found")
	}

	var id int64
	err = rows.Scan(&id)
	if err != nil {
		return -1, fmt.Errorf("Failed to scan ID: %w", err)
	}

	if rows.Next() {
		return -1, fmt.Errorf("More than one row returned")
	}

	err = rows.Err()
	if err != nil {
		return -1, fmt.Errorf("Result set failure: %w", err)
	}

	return id, nil
}

// InternalTokenRecordExists checks if a internal_token_record with the given key exists.
// generator: internal_token_record Exists
func InternalTokenRecordExists(ctx context.Context, tx *sql.Tx, joinerCert string) (bool, error) {
	_, err := GetInternalTokenRecordID(ctx, tx, joinerCert)
	if err != nil {
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// GetInternalTokenRecord returns the internal_token_record with the given key.
// generator: internal_token_record GetOne
func GetInternalTokenRecord(ctx context.Context, tx *sql.Tx, joinerCert string) (*InternalTokenRecord, error) {
	filter := InternalTokenRecordFilter{}
	filter.JoinerCert = &joinerCert

	objects, err := GetInternalTokenRecords(ctx, tx, filter)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch from \"internals_tokens_records\" table: %w", err)
	}

	switch len(objects) {
	case 0:
		return nil, api.StatusErrorf(http.StatusNotFound, "InternalTokenRecord not found")
	case 1:
		return &objects[0], nil
	default:
		return nil, fmt.Errorf("More than one \"internals_tokens_records\" entry matches")
	}
}

// GetInternalTokenRecords returns all available internal_token_records.
// generator: internal_token_record GetMany
func GetInternalTokenRecords(ctx context.Context, tx *sql.Tx, filter InternalTokenRecordFilter) ([]InternalTokenRecord, error) {
	var err error

	// Result slice.
	objects := make([]InternalTokenRecord, 0)

	// Pick the prepared statement and arguments to use based on active criteria.
	var sqlStmt *sql.Stmt
	var args []any

	if filter.JoinerCert != nil && filter.ID == nil && filter.Token == nil {
		sqlStmt = stmt(tx, internalTokenRecordObjectsByJoinerCert)
		args = []any{
			filter.JoinerCert,
		}
	} else if filter.ID == nil && filter.Token == nil && filter.JoinerCert == nil {
		sqlStmt = stmt(tx, internalTokenRecordObjects)
		args = []any{}
	} else {
		return nil, fmt.Errorf("No statement exists for the given Filter")
	}

	// Dest function for scanning a row.
	dest := func(i int) []any {
		objects = append(objects, InternalTokenRecord{})
		return []any{
			&objects[i].ID,
			&objects[i].Token,
			&objects[i].JoinerCert,
		}
	}

	// Select.
	err = query.SelectObjects(sqlStmt, dest, args...)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch from \"internals_tokens_records\" table: %w", err)
	}

	return objects, nil
}

// CreateInternalTokenRecord adds a new internal_token_record to the database.
// generator: internal_token_record Create
func CreateInternalTokenRecord(ctx context.Context, tx *sql.Tx, object InternalTokenRecord) (int64, error) {
	// Check if a internal_token_record with the same key exists.
	exists, err := InternalTokenRecordExists(ctx, tx, object.JoinerCert)
	if err != nil {
		return -1, fmt.Errorf("Failed to check for duplicates: %w", err)
	}

	if exists {
		return -1, api.StatusErrorf(http.StatusConflict, "This \"internals_tokens_records\" entry already exists")
	}

	args := make([]any, 2)

	// Populate the statement arguments.
	args[0] = object.Token
	args[1] = object.JoinerCert

	// Prepared statement to use.
	stmt := stmt(tx, internalTokenRecordCreate)

	// Execute the statement.
	result, err := stmt.Exec(args...)
	if err != nil {
		return -1, fmt.Errorf("Failed to create \"internals_tokens_records\" entry: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return -1, fmt.Errorf("Failed to fetch \"internals_tokens_records\" entry ID: %w", err)
	}

	return id, nil
}

// DeleteInternalTokenRecord deletes the internal_token_record matching the given key parameters.
// generator: internal_token_record DeleteOne-by-JoinerCert
func DeleteInternalTokenRecord(ctx context.Context, tx *sql.Tx, joinerCert string) error {
	stmt := stmt(tx, internalTokenRecordDeleteByJoinerCert)
	result, err := stmt.Exec(joinerCert)
	if err != nil {
		return fmt.Errorf("Delete \"internals_tokens_records\": %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("Fetch affected rows: %w", err)
	}

	if n == 0 {
		return api.StatusErrorf(http.StatusNotFound, "InternalTokenRecord not found")
	} else if n > 1 {
		return fmt.Errorf("Query deleted %d InternalTokenRecord rows instead of 1", n)
	}

	return nil
}
