package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"go.temporal.io/server/common/persistence/sql/sqlplugin"
)

const (
	deleteMapQryTemplate = `DELETE FROM %v
WHERE
shard_id = ? AND
namespace_id = ? AND
workflow_id = ? AND
run_id = ?`

	// %[2]v is the columns of the value struct (i.e. no primary key columns), comma separated
	// %[3]v should be %[2]v with colons prepended.
	// i.e. %[3]v = ",".join(":" + s for s in %[2]v)
	// %[4]v should be %[2]v in the format of n=VALUES(n).
	// i.e. %[4]v = ",".join(s + "=VALUES(" + s + ")" for s in %[2]v)
	// So that this query can be used with BindNamed
	// %[5]v should be the name of the key associated with the map
	// e.g. for ActivityInfo it is "schedule_id"
	setKeyInMapQryTemplate = `INSERT INTO %[1]v
(shard_id, namespace_id, workflow_id, run_id, %[5]v, %[2]v)
VALUES
(:shard_id, :namespace_id, :workflow_id, :run_id, :%[5]v, %[3]v) 
ON DUPLICATE KEY UPDATE %[5]v=VALUES(%[5]v), %[4]v;`

	// %[2]v is the name of the key
	deleteKeyInMapQryTemplate = `DELETE FROM %[1]v
WHERE
shard_id = ? AND
namespace_id = ? AND
workflow_id = ? AND
run_id = ? AND
%[2]v IN ( ? )`

	// %[1]v is the name of the table
	// %[2]v is the name of the key
	// %[3]v is the value columns, separated by commas
	getMapQryTemplate = `SELECT %[2]v, %[3]v FROM %[1]v
WHERE
shard_id = ? AND
namespace_id = ? AND
workflow_id = ? AND
run_id = ?`
)

func stringMap(a []string, f func(string) string) []string {
	b := make([]string, len(a))
	for i, v := range a {
		b[i] = f(v)
	}
	return b
}

func makeDeleteMapQry(tableName string) string {
	return fmt.Sprintf(deleteMapQryTemplate, tableName)
}

func makeSetKeyInMapQry(tableName string, nonPrimaryKeyColumns []string, mapKeyName string) string {
	return fmt.Sprintf(setKeyInMapQryTemplate,
		tableName,
		strings.Join(nonPrimaryKeyColumns, ","),
		strings.Join(stringMap(nonPrimaryKeyColumns, func(x string) string {
			return ":" + x
		}), ","),
		strings.Join(stringMap(nonPrimaryKeyColumns, func(x string) string {
			return x + "=VALUES(" + x + ")"
		}), ","),
		mapKeyName)
}

func makeDeleteKeyInMapQry(tableName string, mapKeyName string) string {
	return fmt.Sprintf(deleteKeyInMapQryTemplate,
		tableName,
		mapKeyName)
}

func makeGetMapQryTemplate(tableName string, nonPrimaryKeyColumns []string, mapKeyName string) string {
	return fmt.Sprintf(getMapQryTemplate,
		tableName,
		mapKeyName,
		strings.Join(nonPrimaryKeyColumns, ","))
}

var (
	// Omit shard_id, run_id, namespace_id, workflow_id, schedule_id since they're in the primary key
	activityInfoColumns = []string{
		"data",
		"data_encoding",
	}
	activityInfoTableName = "activity_info_maps"
	activityInfoKey       = "schedule_id"

	deleteActivityInfoMapQry      = makeDeleteMapQry(activityInfoTableName)
	setKeyInActivityInfoMapQry    = makeSetKeyInMapQry(activityInfoTableName, activityInfoColumns, activityInfoKey)
	deleteKeyInActivityInfoMapQry = makeDeleteKeyInMapQry(activityInfoTableName, activityInfoKey)
	getActivityInfoMapQry         = makeGetMapQryTemplate(activityInfoTableName, activityInfoColumns, activityInfoKey)
)

// ReplaceIntoActivityInfoMaps replaces one or more rows in activity_info_maps table
func (mdb *db) ReplaceIntoActivityInfoMaps(
	ctx context.Context,
	rows []sqlplugin.ActivityInfoMapsRow,
) (sql.Result, error) {
	return mdb.NamedExecContext(ctx,
		setKeyInActivityInfoMapQry,
		rows,
	)
}

// SelectAllFromActivityInfoMaps reads all rows from activity_info_maps table
func (mdb *db) SelectAllFromActivityInfoMaps(
	ctx context.Context,
	filter sqlplugin.ActivityInfoMapsAllFilter,
) ([]sqlplugin.ActivityInfoMapsRow, error) {
	var rows []sqlplugin.ActivityInfoMapsRow
	if err := mdb.SelectContext(ctx,
		&rows,
		getActivityInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	); err != nil {
		return nil, err
	}
	for i := 0; i < len(rows); i++ {
		rows[i].ShardID = filter.ShardID
		rows[i].NamespaceID = filter.NamespaceID
		rows[i].WorkflowID = filter.WorkflowID
		rows[i].RunID = filter.RunID
	}
	return rows, nil
}

// DeleteFromActivityInfoMaps deletes one or more rows from activity_info_maps table
func (mdb *db) DeleteFromActivityInfoMaps(
	ctx context.Context,
	filter sqlplugin.ActivityInfoMapsFilter,
) (sql.Result, error) {
	query, args, err := sqlx.In(
		deleteKeyInActivityInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
		filter.ScheduleIDs,
	)
	if err != nil {
		return nil, err
	}
	return mdb.ExecContext(ctx,
		mdb.Rebind(query),
		args...,
	)
}

// DeleteAllFromActivityInfoMaps deletes all rows from activity_info_maps table
func (mdb *db) DeleteAllFromActivityInfoMaps(
	ctx context.Context,
	filter sqlplugin.ActivityInfoMapsAllFilter,
) (sql.Result, error) {
	return mdb.ExecContext(ctx,
		deleteActivityInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	)
}

var (
	timerInfoColumns = []string{
		"data",
		"data_encoding",
	}
	timerInfoTableName = "timer_info_maps"
	timerInfoKey       = "timer_id"

	deleteTimerInfoMapSQLQuery      = makeDeleteMapQry(timerInfoTableName)
	setKeyInTimerInfoMapSQLQuery    = makeSetKeyInMapQry(timerInfoTableName, timerInfoColumns, timerInfoKey)
	deleteKeyInTimerInfoMapSQLQuery = makeDeleteKeyInMapQry(timerInfoTableName, timerInfoKey)
	getTimerInfoMapSQLQuery         = makeGetMapQryTemplate(timerInfoTableName, timerInfoColumns, timerInfoKey)
)

// ReplaceIntoTimerInfoMaps replaces one or more rows in timer_info_maps table
func (mdb *db) ReplaceIntoTimerInfoMaps(
	ctx context.Context,
	rows []sqlplugin.TimerInfoMapsRow,
) (sql.Result, error) {
	return mdb.NamedExecContext(ctx,
		setKeyInTimerInfoMapSQLQuery,
		rows,
	)
}

// SelectAllFromTimerInfoMaps reads all rows from timer_info_maps table
func (mdb *db) SelectAllFromTimerInfoMaps(
	ctx context.Context,
	filter sqlplugin.TimerInfoMapsAllFilter,
) ([]sqlplugin.TimerInfoMapsRow, error) {
	var rows []sqlplugin.TimerInfoMapsRow
	if err := mdb.SelectContext(ctx,
		&rows,
		getTimerInfoMapSQLQuery,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	); err != nil {
		return nil, err
	}
	for i := 0; i < len(rows); i++ {
		rows[i].ShardID = filter.ShardID
		rows[i].NamespaceID = filter.NamespaceID
		rows[i].WorkflowID = filter.WorkflowID
		rows[i].RunID = filter.RunID
	}
	return rows, nil
}

// DeleteFromTimerInfoMaps deletes one or more rows from timer_info_maps table
func (mdb *db) DeleteFromTimerInfoMaps(
	ctx context.Context,
	filter sqlplugin.TimerInfoMapsFilter,
) (sql.Result, error) {
	query, args, err := sqlx.In(
		deleteKeyInTimerInfoMapSQLQuery,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
		filter.TimerIDs,
	)
	if err != nil {
		return nil, err
	}
	return mdb.ExecContext(ctx,
		mdb.Rebind(query),
		args...,
	)
}

// DeleteAllFromTimerInfoMaps deletes all rows from timer_info_maps table
func (mdb *db) DeleteAllFromTimerInfoMaps(
	ctx context.Context,
	filter sqlplugin.TimerInfoMapsAllFilter,
) (sql.Result, error) {
	return mdb.ExecContext(ctx,
		deleteTimerInfoMapSQLQuery,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	)
}

var (
	childExecutionInfoColumns = []string{
		"data",
		"data_encoding",
	}
	childExecutionInfoTableName = "child_execution_info_maps"
	childExecutionInfoKey       = "initiated_id"

	deleteChildExecutionInfoMapQry      = makeDeleteMapQry(childExecutionInfoTableName)
	setKeyInChildExecutionInfoMapQry    = makeSetKeyInMapQry(childExecutionInfoTableName, childExecutionInfoColumns, childExecutionInfoKey)
	deleteKeyInChildExecutionInfoMapQry = makeDeleteKeyInMapQry(childExecutionInfoTableName, childExecutionInfoKey)
	getChildExecutionInfoMapQry         = makeGetMapQryTemplate(childExecutionInfoTableName, childExecutionInfoColumns, childExecutionInfoKey)
)

// ReplaceIntoChildExecutionInfoMaps replaces one or more rows in child_execution_info_maps table
func (mdb *db) ReplaceIntoChildExecutionInfoMaps(
	ctx context.Context,
	rows []sqlplugin.ChildExecutionInfoMapsRow,
) (sql.Result, error) {
	return mdb.NamedExecContext(ctx,
		setKeyInChildExecutionInfoMapQry,
		rows,
	)
}

// SelectAllFromChildExecutionInfoMaps reads all rows from child_execution_info_maps table
func (mdb *db) SelectAllFromChildExecutionInfoMaps(
	ctx context.Context,
	filter sqlplugin.ChildExecutionInfoMapsAllFilter,
) ([]sqlplugin.ChildExecutionInfoMapsRow, error) {
	var rows []sqlplugin.ChildExecutionInfoMapsRow
	if err := mdb.SelectContext(ctx,
		&rows,
		getChildExecutionInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	); err != nil {
		return nil, err
	}
	for i := 0; i < len(rows); i++ {
		rows[i].ShardID = filter.ShardID
		rows[i].NamespaceID = filter.NamespaceID
		rows[i].WorkflowID = filter.WorkflowID
		rows[i].RunID = filter.RunID
	}
	return rows, nil
}

// DeleteFromChildExecutionInfoMaps deletes one or more rows from child_execution_info_maps table
func (mdb *db) DeleteFromChildExecutionInfoMaps(
	ctx context.Context,
	filter sqlplugin.ChildExecutionInfoMapsFilter,
) (sql.Result, error) {
	query, args, err := sqlx.In(
		deleteKeyInChildExecutionInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
		filter.InitiatedIDs,
	)
	if err != nil {
		return nil, err
	}
	return mdb.ExecContext(ctx,
		mdb.Rebind(query),
		args...,
	)
}

// DeleteAllFromChildExecutionInfoMaps deletes all rows from child_execution_info_maps table
func (mdb *db) DeleteAllFromChildExecutionInfoMaps(
	ctx context.Context,
	filter sqlplugin.ChildExecutionInfoMapsAllFilter,
) (sql.Result, error) {
	return mdb.ExecContext(ctx,
		deleteChildExecutionInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	)
}

var (
	requestCancelInfoColumns = []string{
		"data",
		"data_encoding",
	}
	requestCancelInfoTableName = "request_cancel_info_maps"
	requestCancelInfoKey       = "initiated_id"

	deleteRequestCancelInfoMapQry      = makeDeleteMapQry(requestCancelInfoTableName)
	setKeyInRequestCancelInfoMapQry    = makeSetKeyInMapQry(requestCancelInfoTableName, requestCancelInfoColumns, requestCancelInfoKey)
	deleteKeyInRequestCancelInfoMapQry = makeDeleteKeyInMapQry(requestCancelInfoTableName, requestCancelInfoKey)
	getRequestCancelInfoMapQry         = makeGetMapQryTemplate(requestCancelInfoTableName, requestCancelInfoColumns, requestCancelInfoKey)
)

// ReplaceIntoRequestCancelInfoMaps replaces one or more rows in request_cancel_info_maps table
func (mdb *db) ReplaceIntoRequestCancelInfoMaps(
	ctx context.Context,
	rows []sqlplugin.RequestCancelInfoMapsRow,
) (sql.Result, error) {
	return mdb.NamedExecContext(ctx,
		setKeyInRequestCancelInfoMapQry,
		rows,
	)
}

// SelectAllFromRequestCancelInfoMaps reads all rows from request_cancel_info_maps table
func (mdb *db) SelectAllFromRequestCancelInfoMaps(
	ctx context.Context,
	filter sqlplugin.RequestCancelInfoMapsAllFilter,
) ([]sqlplugin.RequestCancelInfoMapsRow, error) {
	var rows []sqlplugin.RequestCancelInfoMapsRow
	if err := mdb.SelectContext(ctx,
		&rows, getRequestCancelInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	); err != nil {
		return nil, err
	}
	for i := 0; i < len(rows); i++ {
		rows[i].ShardID = filter.ShardID
		rows[i].NamespaceID = filter.NamespaceID
		rows[i].WorkflowID = filter.WorkflowID
		rows[i].RunID = filter.RunID
	}
	return rows, nil
}

// DeleteFromRequestCancelInfoMaps deletes one or more rows from request_cancel_info_maps table
func (mdb *db) DeleteFromRequestCancelInfoMaps(
	ctx context.Context,
	filter sqlplugin.RequestCancelInfoMapsFilter,
) (sql.Result, error) {
	query, args, err := sqlx.In(
		deleteKeyInRequestCancelInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
		filter.InitiatedIDs,
	)
	if err != nil {
		return nil, err
	}
	return mdb.ExecContext(ctx,
		mdb.Rebind(query),
		args...,
	)
}

// DeleteAllFromRequestCancelInfoMaps deletes all rows from request_cancel_info_maps table
func (mdb *db) DeleteAllFromRequestCancelInfoMaps(
	ctx context.Context,
	filter sqlplugin.RequestCancelInfoMapsAllFilter,
) (sql.Result, error) {
	return mdb.ExecContext(ctx,
		deleteRequestCancelInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	)
}

var (
	signalInfoColumns = []string{
		"data",
		"data_encoding",
	}
	signalInfoTableName = "signal_info_maps"
	signalInfoKey       = "initiated_id"

	deleteSignalInfoMapQry      = makeDeleteMapQry(signalInfoTableName)
	setKeyInSignalInfoMapQry    = makeSetKeyInMapQry(signalInfoTableName, signalInfoColumns, signalInfoKey)
	deleteKeyInSignalInfoMapQry = makeDeleteKeyInMapQry(signalInfoTableName, signalInfoKey)
	getSignalInfoMapQry         = makeGetMapQryTemplate(signalInfoTableName, signalInfoColumns, signalInfoKey)
)

// ReplaceIntoSignalInfoMaps replaces one or more rows in signal_info_maps table
func (mdb *db) ReplaceIntoSignalInfoMaps(
	ctx context.Context,
	rows []sqlplugin.SignalInfoMapsRow,
) (sql.Result, error) {
	return mdb.NamedExecContext(ctx,
		setKeyInSignalInfoMapQry,
		rows,
	)
}

// SelectAllFromSignalInfoMaps reads all rows from signal_info_maps table
func (mdb *db) SelectAllFromSignalInfoMaps(
	ctx context.Context,
	filter sqlplugin.SignalInfoMapsAllFilter,
) ([]sqlplugin.SignalInfoMapsRow, error) {
	var rows []sqlplugin.SignalInfoMapsRow
	if err := mdb.SelectContext(ctx,
		&rows,
		getSignalInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	); err != nil {
		return nil, err
	}
	for i := 0; i < len(rows); i++ {
		rows[i].ShardID = filter.ShardID
		rows[i].NamespaceID = filter.NamespaceID
		rows[i].WorkflowID = filter.WorkflowID
		rows[i].RunID = filter.RunID
	}
	return rows, nil
}

// DeleteFromSignalInfoMaps deletes one or more rows from signal_info_maps table
func (mdb *db) DeleteFromSignalInfoMaps(
	ctx context.Context,
	filter sqlplugin.SignalInfoMapsFilter,
) (sql.Result, error) {
	query, args, err := sqlx.In(
		deleteKeyInSignalInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
		filter.InitiatedIDs,
	)
	if err != nil {
		return nil, err
	}
	return mdb.ExecContext(ctx,
		mdb.Rebind(query),
		args...,
	)
}

// DeleteAllFromSignalInfoMaps deletes all rows from signal_info_maps table
func (mdb *db) DeleteAllFromSignalInfoMaps(
	ctx context.Context,
	filter sqlplugin.SignalInfoMapsAllFilter,
) (sql.Result, error) {
	return mdb.ExecContext(ctx,
		deleteSignalInfoMapQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	)
}

const (
	deleteAllSignalsRequestedSetQry = `DELETE FROM signals_requested_sets
WHERE
shard_id = ? AND
namespace_id = ? AND
workflow_id = ? AND
run_id = ?
`

	createSignalsRequestedSetQry = `INSERT INTO signals_requested_sets
(shard_id, namespace_id, workflow_id, run_id, signal_id) VALUES
(:shard_id, :namespace_id, :workflow_id, :run_id, :signal_id)
ON DUPLICATE KEY UPDATE signal_id = VALUES (signal_id)`

	deleteSignalsRequestedSetQry = `DELETE FROM signals_requested_sets
WHERE 
shard_id = ? AND
namespace_id = ? AND
workflow_id = ? AND
run_id = ? AND
signal_id IN ( ? )`

	getSignalsRequestedSetQry = `SELECT signal_id FROM signals_requested_sets WHERE
shard_id = ? AND
namespace_id = ? AND
workflow_id = ? AND
run_id = ?`
)

// InsertIntoSignalsRequestedSets inserts one or more rows into signals_requested_sets table
func (mdb *db) ReplaceIntoSignalsRequestedSets(
	ctx context.Context,
	rows []sqlplugin.SignalsRequestedSetsRow,
) (sql.Result, error) {
	return mdb.NamedExecContext(ctx,
		createSignalsRequestedSetQry,
		rows,
	)

}

// SelectAllFromSignalsRequestedSets reads all rows from signals_requested_sets table
func (mdb *db) SelectAllFromSignalsRequestedSets(
	ctx context.Context,
	filter sqlplugin.SignalsRequestedSetsAllFilter,
) ([]sqlplugin.SignalsRequestedSetsRow, error) {
	var rows []sqlplugin.SignalsRequestedSetsRow
	if err := mdb.SelectContext(ctx,
		&rows,
		getSignalsRequestedSetQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	); err != nil {
		return nil, err
	}
	for i := 0; i < len(rows); i++ {
		rows[i].ShardID = filter.ShardID
		rows[i].NamespaceID = filter.NamespaceID
		rows[i].WorkflowID = filter.WorkflowID
		rows[i].RunID = filter.RunID
	}
	return rows, nil
}

// DeleteFromSignalsRequestedSets deletes one or more rows from signals_requested_sets table
func (mdb *db) DeleteFromSignalsRequestedSets(
	ctx context.Context,
	filter sqlplugin.SignalsRequestedSetsFilter,
) (sql.Result, error) {
	query, args, err := sqlx.In(
		deleteSignalsRequestedSetQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
		filter.SignalIDs,
	)
	if err != nil {
		return nil, err
	}
	return mdb.ExecContext(ctx,
		mdb.Rebind(query),
		args...,
	)
}

// DeleteAllFromSignalsRequestedSets deletes all rows from signals_requested_sets table
func (mdb *db) DeleteAllFromSignalsRequestedSets(
	ctx context.Context,
	filter sqlplugin.SignalsRequestedSetsAllFilter,
) (sql.Result, error) {
	return mdb.ExecContext(ctx,
		deleteAllSignalsRequestedSetQry,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	)
}

var (
	chasmNodeColumns = []string{
		"metadata",
		"metadata_encoding",
		"data",
		"data_encoding",
	}
	chasmNodeTableName = "chasm_node_maps"
	chasmNodeKey       = "chasm_path"

	deleteChasmNodeMapSQLQuery      = makeDeleteMapQry(chasmNodeTableName)
	setKeyInChasmNodeMapSQLQuery    = makeSetKeyInMapQry(chasmNodeTableName, chasmNodeColumns, chasmNodeKey)
	deleteKeyInChasmNodeMapSQLQuery = makeDeleteKeyInMapQry(chasmNodeTableName, chasmNodeKey)
	getChasmNodeMapSQLQuery         = makeGetMapQryTemplate(chasmNodeTableName, chasmNodeColumns, chasmNodeKey)
)

func (mdb *db) SelectAllFromChasmNodeMaps(
	ctx context.Context,
	filter sqlplugin.ChasmNodeMapsAllFilter,
) ([]sqlplugin.ChasmNodeMapsRow, error) {
	var rows []sqlplugin.ChasmNodeMapsRow

	if err := mdb.SelectContext(ctx,
		&rows,
		getChasmNodeMapSQLQuery,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	); err != nil {
		return nil, err
	}

	for i := range rows {
		rows[i].ShardID = filter.ShardID
		rows[i].NamespaceID = filter.NamespaceID
		rows[i].WorkflowID = filter.WorkflowID
		rows[i].RunID = filter.RunID
	}

	return rows, nil
}

func (mdb *db) ReplaceIntoChasmNodeMaps(
	ctx context.Context,
	rows []sqlplugin.ChasmNodeMapsRow,
) (sql.Result, error) {
	return mdb.NamedExecContext(ctx,
		setKeyInChasmNodeMapSQLQuery,
		rows,
	)
}

func (mdb *db) DeleteFromChasmNodeMaps(ctx context.Context, filter sqlplugin.ChasmNodeMapsFilter) (sql.Result, error) {
	query, args, err := sqlx.In(
		deleteKeyInChasmNodeMapSQLQuery,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
		filter.ChasmPaths,
	)
	if err != nil {
		return nil, err
	}
	return mdb.ExecContext(ctx,
		mdb.Rebind(query),
		args...,
	)
}

func (mdb *db) DeleteAllFromChasmNodeMaps(ctx context.Context, filter sqlplugin.ChasmNodeMapsAllFilter) (sql.Result, error) {
	return mdb.ExecContext(ctx,
		deleteChasmNodeMapSQLQuery,
		filter.ShardID,
		filter.NamespaceID,
		filter.WorkflowID,
		filter.RunID,
	)
}
