package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// ddlCapture collects the DDL GORM would send to the database. The migrator
// runs in dry-run mode, so nothing is executed; the statements are only
// rendered for the schema loader.
type ddlCapture struct {
	gormlogger.Interface
	stmts []string
}

// Trace implements the GORM logger interface.
func (c *ddlCapture) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if strings.TrimSpace(sql) != "" {
		c.stmts = append(c.stmts, sql)
	}
}

// SchemaDDL renders the complete DDL of the engine schema: the tables and
// indexes declared by the models followed by the declarative constraints
// from ExtraDDL.
func SchemaDDL() (string, error) {
	capture := &ddlCapture{Interface: gormlogger.Default.LogMode(gormlogger.Silent)}
	db, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "postgres://atlas:atlas@127.0.0.1:5432/atlas?sslmode=disable"}),
		&gorm.Config{
			DryRun:               true,
			DisableAutomaticPing: true,
			Logger:               capture,
		},
	)
	if err != nil {
		return "", fmt.Errorf("open gorm: %w", err)
	}

	tx := db.Session(&gorm.Session{DryRun: true})
	for _, model := range All() {
		if err := tx.Migrator().CreateTable(model); err != nil {
			return "", fmt.Errorf("create table for %T: %w", model, err)
		}
	}

	var b strings.Builder
	for _, stmt := range capture.stmts {
		b.WriteString(stmt)
		b.WriteString(";\n")
	}
	b.WriteString(strings.TrimLeft(ExtraDDL, "\n"))
	return b.String(), nil
}
