package postgresql

import (
	"context"
	"database/sql"
	"errors"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/server/common/persistence/sql/sqlplugin"
)

const (
	createNamespaceQuery = `INSERT INTO 
 namespaces (partition_id, id, name, is_global, data, data_encoding, notification_version)
 VALUES($1, $2, $3, $4, $5, $6, $7)`

	updateNamespaceQuery = `UPDATE namespaces 
 SET name = $1, data = $2, data_encoding = $3, is_global = $4, notification_version = $5
 WHERE partition_id=54321 AND id = $6`

	getNamespacePart = `SELECT id, name, is_global, data, data_encoding, notification_version FROM namespaces`

	getNamespaceByIDQuery   = getNamespacePart + ` WHERE partition_id=$1 AND id = $2`
	getNamespaceByNameQuery = getNamespacePart + ` WHERE partition_id=$1 AND name = $2`

	listNamespacesQuery      = getNamespacePart + ` WHERE partition_id=$1 ORDER BY id LIMIT $2`
	listNamespacesRangeQuery = getNamespacePart + ` WHERE partition_id=$1 AND id > $2 ORDER BY id LIMIT $3`

	deleteNamespaceByIDQuery   = `DELETE FROM namespaces WHERE partition_id=$1 AND id = $2`
	deleteNamespaceByNameQuery = `DELETE FROM namespaces WHERE partition_id=$1 AND name = $2`

	getNamespaceMetadataQuery    = `SELECT notification_version FROM namespace_metadata WHERE partition_id=$1`
	lockNamespaceMetadataQuery   = `SELECT notification_version FROM namespace_metadata WHERE partition_id=$1 FOR UPDATE`
	updateNamespaceMetadataQuery = `UPDATE namespace_metadata SET notification_version = $1 WHERE notification_version = $2 AND partition_id=$3`
)

const (
	partitionID = 54321
)

var errMissingArgs = errors.New("missing one or more args for API")

// InsertIntoNamespace inserts a single row into namespaces table
func (pdb *db) InsertIntoNamespace(
	ctx context.Context,
	row *sqlplugin.NamespaceRow,
) (sql.Result, error) {
	return pdb.ExecContext(ctx, createNamespaceQuery, partitionID, row.ID, row.Name, row.IsGlobal, row.Data, row.DataEncoding, row.NotificationVersion)
}

// UpdateNamespace updates a single row in namespaces table
func (pdb *db) UpdateNamespace(
	ctx context.Context,
	row *sqlplugin.NamespaceRow,
) (sql.Result, error) {
	return pdb.ExecContext(ctx, updateNamespaceQuery, row.Name, row.Data, row.DataEncoding, row.IsGlobal, row.NotificationVersion, row.ID)
}

// SelectFromNamespace reads one or more rows from namespaces table
func (pdb *db) SelectFromNamespace(
	ctx context.Context,
	filter sqlplugin.NamespaceFilter,
) ([]sqlplugin.NamespaceRow, error) {
	var res []sqlplugin.NamespaceRow
	var err error
	switch {
	case filter.ID != nil || filter.Name != nil:
		if filter.ID != nil && filter.Name != nil {
			return nil, serviceerror.NewInternal("only ID or name filter can be specified for selection")
		}
		res, err = pdb.selectFromNamespace(ctx, filter)
	case filter.PageSize != nil && *filter.PageSize > 0:
		res, err = pdb.selectAllFromNamespace(ctx, filter)
	default:
		return nil, errMissingArgs
	}

	return res, err
}

func (pdb *db) selectFromNamespace(
	ctx context.Context,
	filter sqlplugin.NamespaceFilter,
) ([]sqlplugin.NamespaceRow, error) {
	var err error
	var row sqlplugin.NamespaceRow
	switch {
	case filter.ID != nil:
		err = pdb.GetContext(ctx,
			&row,
			getNamespaceByIDQuery,
			partitionID,
			*filter.ID,
		)
	case filter.Name != nil:
		err = pdb.GetContext(ctx,
			&row,
			getNamespaceByNameQuery,
			partitionID,
			*filter.Name,
		)
	}
	if err != nil {
		return nil, err
	}
	return []sqlplugin.NamespaceRow{row}, nil
}

func (pdb *db) selectAllFromNamespace(
	ctx context.Context,
	filter sqlplugin.NamespaceFilter,
) ([]sqlplugin.NamespaceRow, error) {
	var err error
	var rows []sqlplugin.NamespaceRow
	switch {
	case filter.GreaterThanID != nil:
		err = pdb.SelectContext(ctx,
			&rows,
			listNamespacesRangeQuery,
			partitionID,
			*filter.GreaterThanID,
			*filter.PageSize,
		)
	default:
		err = pdb.SelectContext(ctx,
			&rows,
			listNamespacesQuery,
			partitionID,
			filter.PageSize,
		)
	}
	return rows, err
}

// DeleteFromNamespace deletes a single row in namespaces table
func (pdb *db) DeleteFromNamespace(
	ctx context.Context,
	filter sqlplugin.NamespaceFilter,
) (sql.Result, error) {
	var err error
	var result sql.Result
	switch {
	case filter.ID != nil:
		result, err = pdb.ExecContext(ctx,
			deleteNamespaceByIDQuery,
			partitionID,
			filter.ID,
		)
	default:
		result, err = pdb.ExecContext(ctx,
			deleteNamespaceByNameQuery,
			partitionID,
			filter.Name,
		)
	}
	return result, err
}

// LockNamespaceMetadata acquires a write lock on a single row in namespace_metadata table
func (pdb *db) LockNamespaceMetadata(
	ctx context.Context,
) (*sqlplugin.NamespaceMetadataRow, error) {
	var row sqlplugin.NamespaceMetadataRow

	err := pdb.GetContext(ctx,
		&row.NotificationVersion,
		lockNamespaceMetadataQuery,
		partitionID,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SelectFromNamespaceMetadata reads a single row in namespace_metadata table
func (pdb *db) SelectFromNamespaceMetadata(
	ctx context.Context,
) (*sqlplugin.NamespaceMetadataRow, error) {
	var row sqlplugin.NamespaceMetadataRow
	err := pdb.GetContext(ctx,
		&row.NotificationVersion,
		getNamespaceMetadataQuery,
		partitionID,
	)
	return &row, err
}

// UpdateNamespaceMetadata updates a single row in namespace_metadata table
func (pdb *db) UpdateNamespaceMetadata(
	ctx context.Context,
	row *sqlplugin.NamespaceMetadataRow,
) (sql.Result, error) {
	return pdb.ExecContext(ctx,
		updateNamespaceMetadataQuery,
		row.NotificationVersion+1,
		row.NotificationVersion,
		partitionID,
	)
}
