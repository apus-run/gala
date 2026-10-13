package db

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

const (
	errMySQLDuplicatedRecord          uint16 = 1062
	errMySQLForeignKeyConstraintFails uint16 = 1452
)

func IsMySQLDuplicatedRecordErr(err error) bool {
	if mErr, ok := errors.AsType[*mysql.MySQLError](err); ok {
		return mErr.Number == errMySQLDuplicatedRecord
	}
	return false
}

func IsMySQLForeignKeyConstraintFailsError(err error) bool {
	if mErr, ok := errors.AsType[*mysql.MySQLError](err); ok {
		return mErr.Number == errMySQLForeignKeyConstraintFails
	}
	return false
}
