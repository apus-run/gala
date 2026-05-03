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
	var mErr *mysql.MySQLError
	if errors.As(err, &mErr) {
		return mErr.Number == errMySQLDuplicatedRecord
	}
	return false
}

func IsMySQLForeignKeyConstraintFailsError(err error) bool {
	var mErr *mysql.MySQLError
	if errors.As(err, &mErr) {
		return mErr.Number == errMySQLForeignKeyConstraintFails
	}
	return false
}
