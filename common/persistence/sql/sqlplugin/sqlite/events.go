package sqlite

import (
	"context"
	"database/sql"

	"go.temporal.io/server/common/persistence/sql/sqlplugin"
)

const (
	// below are templates for history_node table
	replaceHistoryNodesQuery = `REPLACE INTO history_node (` +
		`shard_id, tree_id, branch_id, node_id, prev_txn_id, txn_id, data, data_encoding) ` +
		`VALUES (:shard_id, :tree_id, :branch_id, :node_id, :prev_txn_id, :txn_id, :data, :data_encoding) `

	getHistoryNodesQuery = `SELECT node_id, prev_txn_id, txn_id, data, data_encoding FROM history_node ` +
		`WHERE shard_id = ? AND tree_id = ? AND branch_id = ? AND ((node_id = ? AND txn_id > ?) OR node_id > ?) AND node_id < ? ` +
		`ORDER BY shard_id, tree_id, branch_id, node_id, txn_id LIMIT ? `

	getHistoryNodesReverseQuery = `SELECT node_id, prev_txn_id, txn_id, data, data_encoding FROM history_node ` +
		`WHERE shard_id = ? AND tree_id = ? AND branch_id = ? AND node_id >= ? AND ((node_id = ? AND txn_id < ?) OR node_id < ?) ` +
		`ORDER BY shard_id, tree_id, branch_id DESC, node_id DESC, txn_id DESC LIMIT ? `

	getHistoryNodeMetadataQuery = `SELECT node_id, prev_txn_id, txn_id FROM history_node ` +
		`WHERE shard_id = ? AND tree_id = ? AND branch_id = ? AND ((node_id = ? AND txn_id > ?) OR node_id > ?) AND node_id < ? ` +
		`ORDER BY shard_id, tree_id, branch_id, node_id, txn_id LIMIT ? `

	deleteHistoryNodeQuery = `DELETE FROM history_node WHERE shard_id = ? AND tree_id = ? AND branch_id = ? AND node_id = ? AND txn_id = ? `

	deleteHistoryNodesQuery = `DELETE FROM history_node WHERE shard_id = ? AND tree_id = ? AND branch_id = ? AND node_id >= ? `

	// below are templates for history_tree table
	addHistoryTreeQuery = `REPLACE INTO history_tree (` +
		`shard_id, tree_id, branch_id, data, data_encoding) ` +
		`VALUES (:shard_id, :tree_id, :branch_id, :data, :data_encoding) `

	getHistoryTreeQuery = `SELECT branch_id, data, data_encoding FROM history_tree WHERE shard_id = ? AND tree_id = ? `

	paginateBranchesQuery = `SELECT shard_id, tree_id, branch_id, data, data_encoding
        FROM history_tree
        WHERE (shard_id, tree_id, branch_id) > ($1, $2, $3)
        ORDER BY shard_id, tree_id, branch_id
        LIMIT $4`

	deleteHistoryTreeQuery = `DELETE FROM history_tree WHERE shard_id = ? AND tree_id = ? AND branch_id = ? `
)

// For history_node table:

// InsertIntoHistoryNode inserts a row into history_node table
func (mdb *db) InsertIntoHistoryNode(
	ctx context.Context,
	row *sqlplugin.HistoryNodeRow,
) (sql.Result, error) {
	// NOTE: txn_id is *= -1 within DB
	row.TxnID = -row.TxnID
	return mdb.conn.NamedExecContext(ctx,
		replaceHistoryNodesQuery,
		row,
	)
}

// DeleteFromHistoryNode delete a row from history_node table
func (mdb *db) DeleteFromHistoryNode(
	ctx context.Context,
	row *sqlplugin.HistoryNodeRow,
) (sql.Result, error) {
	// NOTE: txn_id is *= -1 within DB
	row.TxnID = -row.TxnID
	return mdb.conn.ExecContext(ctx,
		deleteHistoryNodeQuery,
		row.ShardID,
		row.TreeID,
		row.BranchID,
		row.NodeID,
		row.TxnID,
	)
}

// SelectFromHistoryNode reads one or more rows from history_node table
func (mdb *db) RangeSelectFromHistoryNode(
	ctx context.Context,
	filter sqlplugin.HistoryNodeSelectFilter,
) ([]sqlplugin.HistoryNodeRow, error) {
	var query string
	if filter.MetadataOnly {
		query = getHistoryNodeMetadataQuery
	} else if filter.ReverseOrder {
		query = getHistoryNodesReverseQuery
	} else {
		query = getHistoryNodesQuery
	}

	var args []interface{}
	if filter.ReverseOrder {
		args = []interface{}{
			filter.ShardID,
			filter.TreeID,
			filter.BranchID,
			filter.MinNodeID,
			filter.MaxTxnID,
			-filter.MaxTxnID,
			filter.MaxNodeID,
			filter.PageSize,
		}
	} else {
		args = []interface{}{
			filter.ShardID,
			filter.TreeID,
			filter.BranchID,
			filter.MinNodeID,
			-filter.MinTxnID, // NOTE: transaction ID is *= -1 when stored
			filter.MinNodeID,
			filter.MaxNodeID,
			filter.PageSize,
		}
	}

	var rows []sqlplugin.HistoryNodeRow
	if err := mdb.conn.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	for index := range rows {
		rows[index].TxnID = -rows[index].TxnID
	}

	return rows, nil
}

// DeleteFromHistoryNode deletes one or more rows from history_node table
func (mdb *db) RangeDeleteFromHistoryNode(
	ctx context.Context,
	filter sqlplugin.HistoryNodeDeleteFilter,
) (sql.Result, error) {
	return mdb.conn.ExecContext(ctx,
		deleteHistoryNodesQuery,
		filter.ShardID,
		filter.TreeID,
		filter.BranchID,
		filter.MinNodeID,
	)
}

// For history_tree table:

// InsertIntoHistoryTree inserts a row into history_tree table
func (mdb *db) InsertIntoHistoryTree(
	ctx context.Context,
	row *sqlplugin.HistoryTreeRow,
) (sql.Result, error) {
	return mdb.conn.NamedExecContext(ctx,
		addHistoryTreeQuery,
		row,
	)
}

// SelectFromHistoryTree reads one or more rows from history_tree table
func (mdb *db) SelectFromHistoryTree(
	ctx context.Context,
	filter sqlplugin.HistoryTreeSelectFilter,
) ([]sqlplugin.HistoryTreeRow, error) {
	var rows []sqlplugin.HistoryTreeRow
	err := mdb.conn.SelectContext(ctx,
		&rows,
		getHistoryTreeQuery,
		filter.ShardID,
		filter.TreeID,
	)
	return rows, err
}

// PaginateBranchesFromHistoryTree reads up to page.Limit rows from the history_tree table sorted by their primary key,
// while skipping the first page.Offset rows.
func (mdb *db) PaginateBranchesFromHistoryTree(
	ctx context.Context,
	page sqlplugin.HistoryTreeBranchPage,
) ([]sqlplugin.HistoryTreeRow, error) {
	var rows []sqlplugin.HistoryTreeRow
	err := mdb.conn.SelectContext(ctx,
		&rows,
		paginateBranchesQuery,
		page.ShardID,
		page.TreeID,
		page.BranchID,
		page.Limit,
	)
	return rows, err
}

// DeleteFromHistoryTree deletes one or more rows from history_tree table
func (mdb *db) DeleteFromHistoryTree(
	ctx context.Context,
	filter sqlplugin.HistoryTreeDeleteFilter,
) (sql.Result, error) {
	return mdb.conn.ExecContext(ctx,
		deleteHistoryTreeQuery,
		filter.ShardID,
		filter.TreeID,
		filter.BranchID,
	)
}
